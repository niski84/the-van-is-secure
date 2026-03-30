package server

import (
	"encoding/json"
	"fmt"
	"keep-it-mobile/internal/feeds"
	"keep-it-mobile/internal/fred"
	"keep-it-mobile/internal/imgcache"
	"keep-it-mobile/internal/indicators"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ── Response types ────────────────────────────────────────────────────────────

type IndicatorsResponse struct {
	Success     bool                          `json:"success"`
	Data        []*indicators.IndicatorResult `json:"data"`
	StressScore int                           `json:"stress_score"`
	StressBand  string                        `json:"stress_band"`
	VanStatus   string                        `json:"van_status"`
	FetchedAt   string                        `json:"fetched_at"`
	Error       string                        `json:"error,omitempty"`
}

type ArticlesResponse struct {
	Success   bool            `json:"success"`
	Articles  []feeds.Article `json:"articles"`
	FetchedAt string          `json:"fetched_at"`
	Error     string          `json:"error,omitempty"`
}

type ChartPoint struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"`
}

type ChartResponse struct {
	Success bool         `json:"success"`
	Series  string       `json:"series"`
	Points  []ChartPoint `json:"points"`
	Error   string       `json:"error,omitempty"`
}

// ── Server ────────────────────────────────────────────────────────────────────

type cachedIndicators struct {
	data      []*indicators.IndicatorResult
	fetchedAt time.Time
}

type cachedChart struct {
	points    []ChartPoint
	fetchedAt time.Time
}

type Server struct {
	fredClient  *fred.Client
	feedFetcher *feeds.Fetcher
	imgCache    *imgcache.Cache
	mu          sync.Mutex
	cache       *cachedIndicators
	cacheTTL    time.Duration
	chartMu     sync.Mutex
	chartCache  map[string]*cachedChart
	chartTTL    time.Duration
}

func NewServer(fredClient *fred.Client, feedFetcher *feeds.Fetcher, ic *imgcache.Cache) *Server {
	return &Server{
		fredClient:  fredClient,
		feedFetcher: feedFetcher,
		imgCache:    ic,
		cacheTTL:    5 * time.Minute,
		chartCache:  make(map[string]*cachedChart),
		chartTTL:    30 * time.Minute,
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func respondJSON(w http.ResponseWriter, payload interface{}, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}

// Weights must sum to 1.0. Higher-weight series drive the composite score.
var stressWeights = map[string]float64{
	"RECPROUSM156N": 0.18,
	"UNRATE":        0.15,
	"T10Y2Y":        0.12,
	"T10Y3MM":       0.08,
	"BAA10YM":       0.08,
	"MORTGAGE30US":  0.12,
	"DRCCLACBS":     0.10,
	"DROCLACBS":     0.09,
	"CORCCACBS":     0.08,
}

func computeStress(results []*indicators.IndicatorResult) (score int, band string, vanStatus string) {
	statusScore := map[indicators.Status]float64{
		indicators.Green:  0,
		indicators.Yellow: 50,
		indicators.Red:    100,
	}
	var totalW, weighted float64
	for _, r := range results {
		w, ok := stressWeights[r.Series]
		if !ok {
			continue
		}
		weighted += w * statusScore[r.Status]
		totalW += w
	}
	if totalW > 0 {
		score = int(math.Round(weighted / totalW))
	}
	switch {
	case score >= 85:
		band, vanStatus = "CRITICAL", "HIGH STRESS — GET TO THE VAN"
	case score >= 67:
		band, vanStatus = "WARNING", "MOBILITY WINDOW NARROWING"
	case score >= 34:
		band, vanStatus = "ELEVATED", "WATCH CONDITIONS"
	default:
		band, vanStatus = "NORMAL", "THE VAN IS SECURE"
	}
	return
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func HandleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, map[string]interface{}{"success": false, "error": "method not allowed"}, http.StatusMethodNotAllowed)
		return
	}
	respondJSON(w, map[string]interface{}{"success": true, "status": "ok"}, http.StatusOK)
}

func (s *Server) HandleIndicators(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, IndicatorsResponse{Success: false, Error: "method not allowed"}, http.StatusMethodNotAllowed)
		return
	}
	log.Printf("[API] GET /api/indicators")
	results, fetchedAt, err := s.getIndicators()
	if err != nil {
		log.Printf("[API] /api/indicators error: %v", err)
		respondJSON(w, IndicatorsResponse{Success: false, Error: fmt.Sprintf("fetch failed: %v", err)}, http.StatusInternalServerError)
		return
	}
	score, band, vanStatus := computeStress(results)
	respondJSON(w, IndicatorsResponse{
		Success:     true,
		Data:        results,
		StressScore: score,
		StressBand:  band,
		VanStatus:   vanStatus,
		FetchedAt:   fetchedAt.Format(time.RFC3339),
	}, http.StatusOK)
}

func (s *Server) HandleArticles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, ArticlesResponse{Success: false, Error: "method not allowed"}, http.StatusMethodNotAllowed)
		return
	}
	log.Printf("[API] GET /api/articles")
	articles, fetchedAt, err := s.feedFetcher.Articles()
	if err != nil {
		respondJSON(w, ArticlesResponse{Success: false, Error: fmt.Sprintf("fetch failed: %v", err)}, http.StatusInternalServerError)
		return
	}
	respondJSON(w, ArticlesResponse{
		Success:   true,
		Articles:  articles,
		FetchedAt: fetchedAt.Format(time.RFC3339),
	}, http.StatusOK)
}

// HandleChart returns historical observations for a FRED series for charting.
// Query params: series (required), limit (default 52, max 200).
func (s *Server) HandleChart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, ChartResponse{Success: false, Error: "method not allowed"}, http.StatusMethodNotAllowed)
		return
	}

	seriesID := r.URL.Query().Get("series")
	if seriesID == "" {
		respondJSON(w, ChartResponse{Success: false, Error: "series parameter required"}, http.StatusBadRequest)
		return
	}

	limit := 52
	if ls := r.URL.Query().Get("limit"); ls != "" {
		if n, err := strconv.Atoi(ls); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}

	log.Printf("[API] GET /api/chart series=%s limit=%d", seriesID, limit)

	points, err := s.getChart(seriesID, limit)
	if err != nil {
		respondJSON(w, ChartResponse{Success: false, Series: seriesID, Error: err.Error()}, http.StatusInternalServerError)
		return
	}
	respondJSON(w, ChartResponse{Success: true, Series: seriesID, Points: points}, http.StatusOK)
}

// ── Cache ─────────────────────────────────────────────────────────────────────

func (s *Server) getIndicators() ([]*indicators.IndicatorResult, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache != nil && time.Since(s.cache.fetchedAt) < s.cacheTTL {
		log.Printf("[CACHE] indicators: hit (%v)", s.cache.fetchedAt.Format(time.RFC3339))
		return s.cache.data, s.cache.fetchedAt, nil
	}
	log.Printf("[CACHE] indicators: miss — fetching from FRED")
	results, err := fetchAllIndicators(s.fredClient)
	if err != nil {
		return nil, time.Time{}, err
	}
	s.cache = &cachedIndicators{data: results, fetchedAt: time.Now()}
	return s.cache.data, s.cache.fetchedAt, nil
}

func (s *Server) getChart(seriesID string, limit int) ([]ChartPoint, error) {
	cacheKey := fmt.Sprintf("%s:%d", seriesID, limit)
	s.chartMu.Lock()
	defer s.chartMu.Unlock()
	if e, ok := s.chartCache[cacheKey]; ok && time.Since(e.fetchedAt) < s.chartTTL {
		log.Printf("[CACHE] chart %s: hit", cacheKey)
		return e.points, nil
	}
	log.Printf("[CACHE] chart %s: miss — fetching", cacheKey)
	obs, err := s.fredClient.FetchSeries(seriesID, limit)
	if err != nil {
		return nil, err
	}
	// FRED returns desc; reverse to chronological order for charting
	points := make([]ChartPoint, 0, len(obs))
	for i := len(obs) - 1; i >= 0; i-- {
		v, err := fred.ParseFloat(obs[i].Value)
		if err != nil {
			continue
		}
		points = append(points, ChartPoint{Date: obs[i].Date, Value: v})
	}
	s.chartCache[cacheKey] = &cachedChart{points: points, fetchedAt: time.Now()}
	return points, nil
}

// HandleArticleImage resolves an article URL to a locally-cached image path.
// Flow: article URL → fetch/scrape source image URL → download to disk → return /img/cache/{key}.
func (s *Server) HandleArticleImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, map[string]string{"image_url": ""}, http.StatusMethodNotAllowed)
		return
	}
	articleURL := r.URL.Query().Get("url")
	if articleURL == "" {
		respondJSON(w, map[string]string{"image_url": ""}, http.StatusBadRequest)
		return
	}
	// Step 1: get the external source image URL (from RSS feed data or page scrape)
	sourceURL := s.feedFetcher.GetImage(articleURL)
	if sourceURL == "" {
		respondJSON(w, map[string]string{"image_url": ""}, http.StatusOK)
		return
	}
	// Step 2: download to disk cache and return local path
	key, ok := s.imgCache.Get(sourceURL)
	if !ok {
		respondJSON(w, map[string]string{"image_url": ""}, http.StatusOK)
		return
	}
	respondJSON(w, map[string]string{"image_url": "/img/cache/" + key}, http.StatusOK)
}

// HandleCachedImage serves a previously downloaded image from the disk cache.
// Path format: /img/cache/{32-char-hex-key}
func (s *Server) HandleCachedImage(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/img/cache/")
	if len(key) != 32 {
		http.NotFound(w, r)
		return
	}
	for _, ch := range key {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
			http.NotFound(w, r)
			return
		}
	}
	http.ServeFile(w, r, s.imgCache.Path(key))
}

// ── FRED fetch ────────────────────────────────────────────────────────────────

func fetchAllIndicators(client *fred.Client) ([]*indicators.IndicatorResult, error) {
	series := []string{
		// Core recession indicators
		"T10Y2Y", "T10Y3MM", "BAA10YM", "UNRATE", "RECPROUSM156N",
		// Housing
		"MORTGAGE30US", "CUSR0000SEHA", "RRVRUSQ156N",
		// Consumer credit / auto loan health
		"DROCLACBS", "DRCCLACBS", "CORCACBS", "CORCCACBS",
		// Fuel
		"GASREGCOVW",
	}

	var results []*indicators.IndicatorResult

	for _, s := range series {
		log.Printf("[FRED] Processing series: %s", s)
		switch s {
		// ── Core recession indicators ──────────────────────────────────────
		case "T10Y2Y":
			if obs, err := client.FetchSeries(s, 1); err == nil && len(obs) > 0 {
				val, _ := fred.ParseFloat(obs[0].Value)
				res, _ := indicators.ScoreYieldCurve("Yield Curve 10y-2y", s, obs[0].Date, val)
				results = append(results, res)
			}
		case "T10Y3MM":
			if obs, err := client.FetchSeries(s, 1); err == nil && len(obs) > 0 {
				val, _ := fred.ParseFloat(obs[0].Value)
				res, _ := indicators.ScoreYieldCurve("Yield Curve 10y-3m", s, obs[0].Date, val)
				results = append(results, res)
			}
		case "BAA10YM":
			if obs, err := client.FetchSeries(s, 1); err == nil && len(obs) > 0 {
				val, _ := fred.ParseFloat(obs[0].Value)
				res, _ := indicators.ScoreCreditSpread("Credit Spread Baa-10y", s, obs[0].Date, val)
				results = append(results, res)
			}
		case "UNRATE":
			if obs, err := client.FetchSeries(s, 24); err == nil {
				if res, err := indicators.ComputeSahm(obs); err == nil {
					results = append(results, res)
				} else {
					log.Printf("[FRED] Sahm failed: %v", err)
				}
			}
		case "RECPROUSM156N":
			obs, err := client.FetchSeries(s, 1)
			var val float64
			var date string
			if err == nil && len(obs) > 0 {
				val, _ = fred.ParseFloat(obs[0].Value)
				date = obs[0].Date
			}
			res, _ := indicators.ScoreRecessionProb("Recession Probability", s, date, val, err)
			results = append(results, res)

		// ── Housing ───────────────────────────────────────────────────────
		case "MORTGAGE30US":
			if obs, err := client.FetchSeries(s, 1); err == nil && len(obs) > 0 {
				val, _ := fred.ParseFloat(obs[0].Value)
				res, _ := indicators.ScoreMortgageRate("30-Yr Mortgage Rate", s, obs[0].Date, val)
				results = append(results, res)
			}
		case "CUSR0000SEHA":
			if obs, err := client.FetchSeries(s, 14); err == nil {
				if res, err := indicators.ScoreRentYoY(obs); err == nil {
					results = append(results, res)
				} else {
					log.Printf("[FRED] RentYoY failed: %v", err)
				}
			}
		case "RRVRUSQ156N":
			if obs, err := client.FetchSeries(s, 1); err == nil && len(obs) > 0 {
				val, _ := fred.ParseFloat(obs[0].Value)
				res, _ := indicators.ScoreRentalVacancy("Rental Vacancy Rate", s, obs[0].Date, val)
				results = append(results, res)
			}

		// ── Consumer Credit / Auto Loan Health ───────────────────────────
		case "DROCLACBS":
			if obs, err := client.FetchSeries(s, 1); err == nil && len(obs) > 0 {
				val, _ := fred.ParseFloat(obs[0].Value)
				res, _ := indicators.ScoreConsumerDelinquency("Auto & Consumer Loans 30+ DPD", s, obs[0].Date, val, 2.8, 1.8)
				results = append(results, res)
			}
		case "DRCCLACBS":
			if obs, err := client.FetchSeries(s, 1); err == nil && len(obs) > 0 {
				val, _ := fred.ParseFloat(obs[0].Value)
				res, _ := indicators.ScoreConsumerDelinquency("Credit Card Delinquency", s, obs[0].Date, val, 3.5, 2.5)
				results = append(results, res)
			}
		case "CORCACBS":
			if obs, err := client.FetchSeries(s, 1); err == nil && len(obs) > 0 {
				val, _ := fred.ParseFloat(obs[0].Value)
				res, _ := indicators.ScoreChargeOffRate("Consumer Loan Charge-Offs", s, obs[0].Date, val, 3.5, 2.0)
				results = append(results, res)
			}
		case "CORCCACBS":
			if obs, err := client.FetchSeries(s, 1); err == nil && len(obs) > 0 {
				val, _ := fred.ParseFloat(obs[0].Value)
				res, _ := indicators.ScoreChargeOffRate("Credit Card Charge-Offs", s, obs[0].Date, val, 5.0, 3.5)
				results = append(results, res)
			}

		// ── Fuel ─────────────────────────────────────────────────────────
		case "GASREGCOVW":
			if obs, err := client.FetchSeries(s, 1); err == nil && len(obs) > 0 {
				val, _ := fred.ParseFloat(obs[0].Value)
				res, _ := indicators.ScoreGasPrice("Regular Gasoline", s, obs[0].Date, val)
				results = append(results, res)
			}
		}
	}

	return results, nil
}
