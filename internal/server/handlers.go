package server

import (
	"encoding/json"
	"fmt"
	"keep-it-mobile/internal/crypto"
	"keep-it-mobile/internal/feeds"
	"keep-it-mobile/internal/fred"
	"keep-it-mobile/internal/imgcache"
	"keep-it-mobile/internal/indicators"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ── Crypto types ──────────────────────────────────────────────────────────────

type CryptoMetric struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Value          float64        `json:"value"`
	Change24h      float64        `json:"change_24h,omitempty"`
	Label          string         `json:"label"`
	Classification string         `json:"classification,omitempty"`
	Status         string         `json:"status"` // GREEN / YELLOW / RED
	Points         []crypto.Point `json:"points"`
}

type CryptoResponse struct {
	Success   bool           `json:"success"`
	Metrics   []CryptoMetric `json:"metrics"`
	FetchedAt string         `json:"fetched_at"`
	Error     string         `json:"error,omitempty"`
}

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

type commodityDef struct {
	Series   string `json:"series"`
	Name     string `json:"name"`
	Unit     string `json:"unit"`
	Prefix   string `json:"prefix"`
	Decimals int    `json:"decimals"`
	Icon     string `json:"icon"`
}

type CommodityResult struct {
	commodityDef
	Current   float64      `json:"current"`
	Date      string       `json:"date"`
	PctChange float64      `json:"pct_change"` // positive = price up (bad), negative = price down (good)
	Points    []ChartPoint `json:"points"`
}

type CommoditiesResponse struct {
	Success bool              `json:"success"`
	Months  int               `json:"months"`
	Top     int               `json:"top"`
	Total   int               `json:"total"`   // how many series had data (before top-N filter)
	Results []CommodityResult `json:"results"` // sorted by abs(pct_change) desc — biggest movers first
	Error   string            `json:"error,omitempty"`
}

// commodityCatalog is the master set of BLS average-price series tracked for mover analysis.
// Series that fail to load (FRED error / no data) are silently skipped.
var commodityCatalog = []commodityDef{
	// Proteins
	{Series: "APU0000708111", Name: "Eggs, Grade A Large",     Unit: "per doz",    Prefix: "$", Decimals: 3, Icon: "🥚"},
	{Series: "APU0000703112", Name: "Ground Beef",              Unit: "per lb",     Prefix: "$", Decimals: 3, Icon: "🥩"},
	{Series: "APU0000704111", Name: "Bacon, Sliced",            Unit: "per lb",     Prefix: "$", Decimals: 3, Icon: "🥓"},
	{Series: "APU0000706111", Name: "Chicken, Whole",           Unit: "per lb",     Prefix: "$", Decimals: 3, Icon: "🍗"},
	{Series: "APU0000FC1101", Name: "Chicken Breast, Boneless", Unit: "per lb",     Prefix: "$", Decimals: 3, Icon: "🍖"},
	// Dairy
	{Series: "APU0000709112", Name: "Whole Milk",               Unit: "per gal",    Prefix: "$", Decimals: 3, Icon: "🥛"},
	{Series: "APU0000715211", Name: "Butter, Salted",           Unit: "per lb",     Prefix: "$", Decimals: 3, Icon: "🧈"},
	// Staples
	{Series: "APU0000FF1101", Name: "White Bread",              Unit: "per lb",     Prefix: "$", Decimals: 3, Icon: "🍞"},
	{Series: "APU0000717311", Name: "Coffee, Ground Roast",     Unit: "per lb",     Prefix: "$", Decimals: 3, Icon: "☕"},
	{Series: "APU0000712112", Name: "Sugar, White",             Unit: "per lb",     Prefix: "$", Decimals: 3, Icon: "🍬"},
	{Series: "APU0000711211", Name: "Potatoes, White",          Unit: "per lb",     Prefix: "$", Decimals: 3, Icon: "🥔"},
	// Produce
	{Series: "APU0000711311", Name: "Orange Juice, Frozen",     Unit: "per 16oz",   Prefix: "$", Decimals: 3, Icon: "🍊"},
	{Series: "APU0000712311", Name: "Apples, Red Delicious",    Unit: "per lb",     Prefix: "$", Decimals: 3, Icon: "🍎"},
	// Energy
	{Series: "GASREGCOVW",    Name: "Gasoline, Regular",        Unit: "per gal",    Prefix: "$", Decimals: 3, Icon: "⛽"},
	// Broad index
	{Series: "CUSR0000SAF11", Name: "Food at Home (CPI)",       Unit: "index",      Prefix: "",  Decimals: 1, Icon: "🛒"},
}

// metalsCatalog tracks precious metals price indices from FRED.
// FRED removed LBMA spot prices in 2022; these NASDAQ FLOWS indices are the
// best available proxies — values closely track market prices directionally.
var metalsCatalog = []commodityDef{
	{Series: "NASDAQQGLDI", Name: "Gold",   Unit: "FLOWS index", Prefix: "$", Decimals: 2, Icon: "🥇"},
	{Series: "NASDAQQSLVO", Name: "Silver", Unit: "FLOWS index", Prefix: "",  Decimals: 2, Icon: "🥈"},
}

// joltsCatalog tracks JOLTS job-openings series by industry (BLS via FRED).
// Values are in thousands of job openings, seasonally adjusted monthly.
// Sorted by biggest % change over the requested window (same pattern as commodities).
var joltsCatalog = []commodityDef{
	{Series: "JTSJOL",       Name: "Total Nonfarm",        Unit: "K openings", Prefix: "", Decimals: 0, Icon: "🏢"},
	{Series: "JTU5200JOL",   Name: "Finance & Insurance",  Unit: "K openings", Prefix: "", Decimals: 0, Icon: "💳"},
	{Series: "JTS6200JOL",   Name: "Health Care",          Unit: "K openings", Prefix: "", Decimals: 0, Icon: "🏥"},
	{Series: "JTU5100JOL",   Name: "Information",          Unit: "K openings", Prefix: "", Decimals: 0, Icon: "💻"},
	{Series: "JTS3000JOL",   Name: "Manufacturing",        Unit: "K openings", Prefix: "", Decimals: 0, Icon: "🏭"},
	{Series: "JTS4400JOL",   Name: "Retail Trade",         Unit: "K openings", Prefix: "", Decimals: 0, Icon: "🛍️"},
	{Series: "JTS540099JOL", Name: "Professional Svcs",    Unit: "K openings", Prefix: "", Decimals: 0, Icon: "💼"},
	{Series: "JTS2300JOL",   Name: "Construction",         Unit: "K openings", Prefix: "", Decimals: 0, Icon: "🏗️"},
}

type employmentDef struct {
	Series string
	Name   string
	Icon   string
}

// employmentCatalog tracks BLS Establishment Survey (Table B-1) payroll series.
// Values are in thousands of employees, seasonally adjusted monthly.
var employmentCatalog = []employmentDef{
	{Series: "PAYEMS",        Name: "Total Nonfarm",        Icon: "🏛️"},
	{Series: "MANEMP",        Name: "Manufacturing",        Icon: "🏭"},
	{Series: "USTRADE",       Name: "Retail Trade",         Icon: "🛍️"},
	{Series: "USCONS",        Name: "Construction",         Icon: "🏗️"},
	{Series: "USINFO",        Name: "Information",          Icon: "💻"},
	{Series: "CES5552000001", Name: "Finance & Insurance",  Icon: "💳"},
	{Series: "CES6562000001", Name: "Health Care",          Icon: "🏥"},
	{Series: "USPBS",         Name: "Prof. & Business Svcs",Icon: "💼"},
}

// EmploymentResult holds a single industry's payroll level and month-over-month delta.
type EmploymentResult struct {
	Series    string       `json:"series"`
	Name      string       `json:"name"`
	Icon      string       `json:"icon"`
	Current   float64      `json:"current"`    // thousands of employees
	Date      string       `json:"date"`
	NetChange float64      `json:"net_change"` // MoM delta, thousands
	Points    []ChartPoint `json:"points"`      // chronological history for sparkline
}

// EmploymentResponse is the /api/employment response envelope.
type EmploymentResponse struct {
	Success   bool               `json:"success"`
	Results   []EmploymentResult `json:"results"` // sorted by abs(net_change) desc
	FetchedAt string             `json:"fetched_at"`
	Error     string             `json:"error,omitempty"`
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

type cachedCrypto struct {
	data      *CryptoResponse
	fetchedAt time.Time
}

type Server struct {
	fredClient   *fred.Client
	cryptoClient *crypto.Client
	feedFetcher  *feeds.Fetcher
	imgCache     *imgcache.Cache
	mu           sync.Mutex
	cache        *cachedIndicators
	cacheTTL     time.Duration
	chartMu      sync.Mutex
	chartCache   map[string]*cachedChart
	chartTTL     time.Duration
	// chartCacheMax caps how many series:limit entries we keep in memory.
	// When we exceed it, expired entries are swept first; if still over, the map is reset.
	chartCacheMax int
	cryptoMu    sync.Mutex
	cryptoCache *cachedCrypto
	cryptoTTL   time.Duration
}

func NewServer(fredClient *fred.Client, feedFetcher *feeds.Fetcher, ic *imgcache.Cache) *Server {
	s := &Server{
		fredClient:    fredClient,
		cryptoClient:  crypto.NewClient(15 * time.Second),
		feedFetcher:   feedFetcher,
		imgCache:      ic,
		cacheTTL:      6 * time.Hour,
		chartCache:    make(map[string]*cachedChart),
		chartTTL:      24 * time.Hour,
		chartCacheMax: 200,
		cryptoTTL:     1 * time.Hour, // crypto updates frequently
	}
	go s.evictLoop()
	return s
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func respondJSON(w http.ResponseWriter, payload interface{}, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}

// Weights must sum to 1.0. Higher-weight series drive the composite score.
var stressWeights = map[string]float64{
	// Recession signals
	"RECPROUSM156N": 0.10,
	"SAHMREALTIME":  0.08,
	"ICSA":          0.05,
	// Financial stress
	"STLFSI4":       0.05,
	"NFCI":          0.04,
	// Yield curve / rates
	"T10Y2Y":        0.07,
	"T10Y3MM":       0.05,
	"BAA10YM":       0.05,
	"T10YIE":        0.04,
	// Labor
	"UNRATE":        0.08,
	"U6RATE":        0.04,
	// Markets
	"VIXCLS":        0.04,
	"SP500":         0.03,
	// Housing
	"MORTGAGE30US":  0.06,
	"CUSR0000SEHA":  0.03,
	"CSUSHPISA":     0.03,
	// Mortgage / foreclosure
	"DRSFRMACBS":          0.05,
	"RCMFLBBALDPDPCT90P":  0.04,
	"RCMFLBBALDPDPCT30P":  0.02,
	// Consumer credit
	"DRCCLACBS":     0.04,
	"DROCLACBS":     0.03,
	"CORCCACBS":     0.03,
	// Consumer health
	"PSAVERT":       0.03,
	"UMCSENT":       0.03,
	// Debt & fiscal
	"GFDEGDQ188S":   0.03,
	// Energy
	"DCOILWTICO":    0.03,
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

// HandleCommodities fetches the full commodity catalog concurrently, ranks by
// absolute % change over the requested window, and returns the top movers.
// Query params:
//   - months: lookback window in months (default 12, max 120)
//   - top:    max results to return   (default 8,  max 30)
func (s *Server) HandleCommodities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, CommoditiesResponse{Success: false, Error: "method not allowed"}, http.StatusMethodNotAllowed)
		return
	}

	months := 12
	if m := r.URL.Query().Get("months"); m != "" {
		if n, err := strconv.Atoi(m); err == nil && n > 0 && n <= 120 {
			months = n
		}
	}
	top := 8
	if t := r.URL.Query().Get("top"); t != "" {
		if n, err := strconv.Atoi(t); err == nil && n > 0 && n <= 30 {
			top = n
		}
	}

	log.Printf("[API] GET /api/commodities months=%d top=%d", months, top)

	// Fetch all catalog series concurrently; each reuses the chart cache.
	type fetchResult struct {
		def    commodityDef
		points []ChartPoint
		err    error
	}
	results := make(chan fetchResult, len(commodityCatalog))
	for _, def := range commodityCatalog {
		def := def
		go func() {
			pts, err := s.getChart(def.Series, months)
			results <- fetchResult{def: def, points: pts, err: err}
		}()
	}

	var movers []CommodityResult
	for range commodityCatalog {
		fr := <-results
		if fr.err != nil {
			log.Printf("[COMMODITIES] series %s error: %v", fr.def.Series, fr.err)
			continue
		}
		if len(fr.points) < 2 {
			continue
		}
		first := fr.points[0]
		last := fr.points[len(fr.points)-1]
		pct := (last.Value - first.Value) / first.Value * 100
		movers = append(movers, CommodityResult{
			commodityDef: fr.def,
			Current:      last.Value,
			Date:         last.Date,
			PctChange:    pct,
			Points:       fr.points,
		})
	}

	// Sort biggest absolute movers first.
	sort.Slice(movers, func(i, j int) bool {
		return math.Abs(movers[i].PctChange) > math.Abs(movers[j].PctChange)
	})

	total := len(movers)
	if top < len(movers) {
		movers = movers[:top]
	}

	respondJSON(w, CommoditiesResponse{
		Success: true,
		Months:  months,
		Top:     top,
		Total:   total,
		Results: movers,
	}, http.StatusOK)
}

// HandleMetals returns all precious metals from metalsCatalog with % change and chart points.
// Query params:
//   - months: lookback window in months (default 6, max 120)
func (s *Server) HandleMetals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, CommoditiesResponse{Success: false, Error: "method not allowed"}, http.StatusMethodNotAllowed)
		return
	}

	months := 6
	if m := r.URL.Query().Get("months"); m != "" {
		if n, err := strconv.Atoi(m); err == nil && n > 0 && n <= 120 {
			months = n
		}
	}

	log.Printf("[API] GET /api/metals months=%d", months)

	// NASDAQQGLDI / NASDAQQSLVO are daily series — convert months to ~trading days.
	limit := months * 22
	if limit > 200 {
		limit = 200 // chart cache cap
	}

	type fetchResult struct {
		def    commodityDef
		points []ChartPoint
		err    error
	}
	results := make(chan fetchResult, len(metalsCatalog))
	for _, def := range metalsCatalog {
		def := def
		go func() {
			pts, err := s.getChart(def.Series, limit)
			results <- fetchResult{def: def, points: pts, err: err}
		}()
	}

	var metals []CommodityResult
	for range metalsCatalog {
		fr := <-results
		if fr.err != nil {
			log.Printf("[METALS] series %s error: %v", fr.def.Series, fr.err)
			continue
		}
		if len(fr.points) < 2 {
			continue
		}
		first := fr.points[0]
		last := fr.points[len(fr.points)-1]
		pct := (last.Value - first.Value) / first.Value * 100
		metals = append(metals, CommodityResult{
			commodityDef: fr.def,
			Current:      last.Value,
			Date:         last.Date,
			PctChange:    pct,
			Points:       fr.points,
		})
	}

	// Sort by series order (preserve catalog order for metals)
	sort.Slice(metals, func(i, j int) bool {
		return metals[i].Series < metals[j].Series
	})

	respondJSON(w, CommoditiesResponse{
		Success: true,
		Months:  months,
		Top:     len(metals),
		Total:   len(metals),
		Results: metals,
	}, http.StatusOK)
}

// HandleJOLTS returns JOLTS job-opening series ranked by biggest % change over
// the requested window. Structurally identical to HandleCommodities.
// Query params:
//   - months: lookback window in months (default 24, max 60)
//   - top:    max results to return    (default 8,  max 12)
func (s *Server) HandleJOLTS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, CommoditiesResponse{Success: false, Error: "method not allowed"}, http.StatusMethodNotAllowed)
		return
	}

	months := 24
	if m := r.URL.Query().Get("months"); m != "" {
		if n, err := strconv.Atoi(m); err == nil && n > 0 && n <= 60 {
			months = n
		}
	}
	top := 8
	if t := r.URL.Query().Get("top"); t != "" {
		if n, err := strconv.Atoi(t); err == nil && n > 0 && n <= 12 {
			top = n
		}
	}

	log.Printf("[API] GET /api/jolts months=%d top=%d", months, top)

	type fetchResult struct {
		def    commodityDef
		points []ChartPoint
		err    error
	}
	results := make(chan fetchResult, len(joltsCatalog))
	for _, def := range joltsCatalog {
		def := def
		go func() {
			pts, err := s.getChart(def.Series, months)
			results <- fetchResult{def: def, points: pts, err: err}
		}()
	}

	var movers []CommodityResult
	for range joltsCatalog {
		fr := <-results
		if fr.err != nil {
			log.Printf("[JOLTS] series %s error: %v", fr.def.Series, fr.err)
			continue
		}
		if len(fr.points) < 2 {
			continue
		}
		first := fr.points[0]
		last := fr.points[len(fr.points)-1]
		pct := (last.Value - first.Value) / first.Value * 100
		movers = append(movers, CommodityResult{
			commodityDef: fr.def,
			Current:      last.Value,
			Date:         last.Date,
			PctChange:    pct,
			Points:       fr.points,
		})
	}

	sort.Slice(movers, func(i, j int) bool {
		return math.Abs(movers[i].PctChange) > math.Abs(movers[j].PctChange)
	})

	total := len(movers)
	if top < len(movers) {
		movers = movers[:top]
	}

	respondJSON(w, CommoditiesResponse{
		Success: true,
		Months:  months,
		Top:     top,
		Total:   total,
		Results: movers,
	}, http.StatusOK)
}

// HandleEmployment returns BLS Table B-1 payroll levels with month-over-month
// net change, sorted by absolute change (biggest movers first).
// Always fetches 13 monthly observations so the sparkline covers 12 months.
func (s *Server) HandleEmployment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, EmploymentResponse{Success: false, Error: "method not allowed"}, http.StatusMethodNotAllowed)
		return
	}

	log.Printf("[API] GET /api/employment")

	type fetchResult struct {
		def employmentDef
		pts []ChartPoint
		err error
	}
	ch := make(chan fetchResult, len(employmentCatalog))
	for _, def := range employmentCatalog {
		def := def
		go func() {
			pts, err := s.getChart(def.Series, 13) // 13 obs → 12 visible monthly deltas
			ch <- fetchResult{def: def, pts: pts, err: err}
		}()
	}

	var out []EmploymentResult
	for range employmentCatalog {
		fr := <-ch
		if fr.err != nil {
			log.Printf("[EMPLOYMENT] series %s error: %v", fr.def.Series, fr.err)
			continue
		}
		if len(fr.pts) < 2 {
			continue
		}
		last := fr.pts[len(fr.pts)-1]
		prev := fr.pts[len(fr.pts)-2]
		out = append(out, EmploymentResult{
			Series:    fr.def.Series,
			Name:      fr.def.Name,
			Icon:      fr.def.Icon,
			Current:   last.Value,
			Date:      last.Date,
			NetChange: last.Value - prev.Value,
			Points:    fr.pts,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		return math.Abs(out[i].NetChange) > math.Abs(out[j].NetChange)
	})

	respondJSON(w, EmploymentResponse{
		Success:   true,
		Results:   out,
		FetchedAt: time.Now().Format(time.RFC3339),
	}, http.StatusOK)
}

// ── Cache ─────────────────────────────────────────────────────────────────────

func (s *Server) getIndicators() ([]*indicators.IndicatorResult, time.Time, error) {
	s.mu.Lock()
	if s.cache != nil && time.Since(s.cache.fetchedAt) < s.cacheTTL {
		data, at := s.cache.data, s.cache.fetchedAt
		s.mu.Unlock()
		log.Printf("[CACHE] indicators: hit (%v)", at.Format(time.RFC3339))
		return data, at, nil
	}
	s.mu.Unlock()

	// Fetch outside the lock so concurrent requests don't block each other.
	// Two simultaneous misses will both fetch — acceptable given the low traffic.
	log.Printf("[CACHE] indicators: miss — fetching from FRED")
	results, err := fetchAllIndicators(s.fredClient)
	if err != nil {
		return nil, time.Time{}, err
	}
	now := time.Now()
	s.mu.Lock()
	s.cache = &cachedIndicators{data: results, fetchedAt: now}
	s.mu.Unlock()
	return results, now, nil
}

func (s *Server) getChart(seriesID string, limit int) ([]ChartPoint, error) {
	cacheKey := fmt.Sprintf("%s:%d", seriesID, limit)

	s.chartMu.Lock()
	if e, ok := s.chartCache[cacheKey]; ok && time.Since(e.fetchedAt) < s.chartTTL {
		pts := e.points
		s.chartMu.Unlock()
		log.Printf("[CACHE] chart %s: hit", cacheKey)
		return pts, nil
	}
	s.chartMu.Unlock()

	// Fetch outside the lock so other series can be served from cache concurrently.
	log.Printf("[CACHE] chart %s: miss — fetching from FRED", cacheKey)
	obs, err := s.fredClient.FetchSeries(seriesID, limit)
	if err != nil {
		return nil, err
	}
	// FRED returns desc; reverse to chronological order for charting.
	points := make([]ChartPoint, 0, len(obs))
	for i := len(obs) - 1; i >= 0; i-- {
		v, err := fred.ParseFloat(obs[i].Value)
		if err != nil {
			continue
		}
		points = append(points, ChartPoint{Date: obs[i].Date, Value: v})
	}

	s.chartMu.Lock()
	// Size guard: sweep expired entries before inserting.
	if len(s.chartCache) >= s.chartCacheMax {
		s.sweepExpiredCharts()
		if len(s.chartCache) >= s.chartCacheMax {
			// Still at cap after sweep — reset rather than refuse to cache.
			log.Printf("[CACHE] chart cache at cap (%d); resetting", s.chartCacheMax)
			s.chartCache = make(map[string]*cachedChart)
		}
	}
	s.chartCache[cacheKey] = &cachedChart{points: points, fetchedAt: time.Now()}
	s.chartMu.Unlock()
	return points, nil
}

// sweepExpiredCharts deletes entries older than chartTTL. Caller must hold chartMu.
func (s *Server) sweepExpiredCharts() {
	for k, e := range s.chartCache {
		if time.Since(e.fetchedAt) >= s.chartTTL {
			delete(s.chartCache, k)
		}
	}
}

// WarmCache pre-fetches every series the dashboard needs, staggered to stay well within
// FRED's rate limits. Call once at startup in a goroutine. The daily re-warm timer in
// main.go ensures deployed instances never go more than ~24 h without fresh data.
func (s *Server) WarmCache() {
	log.Printf("[CACHE] warm-up starting")

	// Indicators first — covers the main dashboard load.
	if _, _, err := s.getIndicators(); err != nil {
		log.Printf("[CACHE] warm indicators: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	// Chart series with their default limits used by the UI.
	type entry struct {
		series string
		limit  int
	}
	entries := []entry{
		// Van Readiness section
		{"GASREGCOVW", 52},
		{"RRVRUSQ156N", 8},
		// Consumer Credit section (8-quarter trend cards)
		{"DROCLACBS", 8},
		{"DRCCLACBS", 8},
		{"CORCACBS", 8},
		{"CORCCACBS", 8},
		// Indicator mini charts
		{"RECPROUSM156N", 24},
		{"SAHMREALTIME", 24},
		{"ICSA", 52},
		{"STLFSI4", 52},
		{"NFCI", 52},
		{"T10Y2Y", 52},
		{"T10Y3MM", 52},
		{"BAA10YM", 52},
		{"T10YIE", 52},
		{"UNRATE", 24},
		{"U6RATE", 24},
		{"VIXCLS", 52},
		{"SP500", 52},
		{"MORTGAGE30US", 52},
		{"CUSR0000SEHA", 24},
		{"CSUSHPISA", 24},
		// Mortgage / foreclosure (quarterly series — 16 obs ≈ 4 years)
		{"DRSFRMACBS", 16},
		{"RCMFLBBALDPDPCT30P", 16},
		{"RCMFLBBALDPDPCT90P", 16},
		{"PSAVERT", 24},
		{"UMCSENT", 24},
		{"GFDEGDQ188S", 16},
		{"DCOILWTICO", 52},
		{"DTWEXBGS", 270},
		{"TOTALNS", 24},
	}
	// Commodity catalog — warm both common time-range windows.
	for _, def := range commodityCatalog {
		entries = append(entries, entry{def.Series, 12})  // 1Y
		entries = append(entries, entry{def.Series, 24})  // 2Y (default)
	}
	// Precious metals — daily series; warm 6M (132 obs) and 9M (198 obs)
	for _, def := range metalsCatalog {
		entries = append(entries, entry{def.Series, 132})
		entries = append(entries, entry{def.Series, 198})
	}
	// JOLTS job openings — monthly series; warm 12M and 24M windows
	for _, def := range joltsCatalog {
		entries = append(entries, entry{def.Series, 12})
		entries = append(entries, entry{def.Series, 24})
	}
	// Employment (Table B-1) — 13 obs = 12 monthly deltas + sparkline
	for _, def := range employmentCatalog {
		entries = append(entries, entry{def.Series, 13})
	}
	// Industry deep-dive — additional leading-indicator series not warmed above
	entries = append(entries,
		entry{"IHLIDXUSTPSOFTDEVE", 52}, // Indeed software dev postings — weekly (~1 yr)
		entry{"IPMAN",             24},  // Industrial production: manufacturing
		entry{"HOUST",             24},  // Housing starts
		entry{"PERMIT",            24},  // Building permits
		entry{"TEMPHELPS",         24},  // Temporary help services employment
	)

	for i, e := range entries {
		if _, err := s.getChart(e.series, e.limit); err != nil {
			log.Printf("[CACHE] warm chart %s:%d: %v", e.series, e.limit, err)
		}
		// Stagger requests — 200 ms between each keeps burst below ~5 req/s.
		// Semaphore in the FRED client further limits to 3 concurrent.
		if i < len(entries)-1 {
			time.Sleep(200 * time.Millisecond)
		}
	}
	log.Printf("[CACHE] warm-up complete (%d chart entries)", len(entries))

	// Crypto — separate from FRED, warm independently
	if _, err := s.getCrypto(); err != nil {
		log.Printf("[CACHE] warm crypto: %v", err)
	}
}

// HandleCrypto returns crypto market metrics with historical sparkline data.
func (s *Server) HandleCrypto(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, CryptoResponse{Success: false, Error: "method not allowed"}, http.StatusMethodNotAllowed)
		return
	}
	log.Printf("[API] GET /api/crypto")
	resp, err := s.getCrypto()
	if err != nil {
		log.Printf("[API] /api/crypto error: %v", err)
		respondJSON(w, CryptoResponse{Success: false, Error: err.Error()}, http.StatusInternalServerError)
		return
	}
	respondJSON(w, resp, http.StatusOK)
}

func (s *Server) getCrypto() (*CryptoResponse, error) {
	s.cryptoMu.Lock()
	if s.cryptoCache != nil && time.Since(s.cryptoCache.fetchedAt) < s.cryptoTTL {
		resp := s.cryptoCache.data
		s.cryptoMu.Unlock()
		log.Printf("[CACHE] crypto: hit")
		return resp, nil
	}
	s.cryptoMu.Unlock()

	log.Printf("[CACHE] crypto: miss — fetching")
	resp, err := s.fetchCrypto()
	if err != nil {
		return nil, err
	}
	s.cryptoMu.Lock()
	s.cryptoCache = &cachedCrypto{data: resp, fetchedAt: time.Now()}
	s.cryptoMu.Unlock()
	return resp, nil
}

func (s *Server) fetchCrypto() (*CryptoResponse, error) {
	type result struct {
		metrics []CryptoMetric // goroutines may yield 1 or 2 metrics
		err     error
		key     string
	}
	// 4 goroutines; global yields 2 metrics so buffer = 4 slots
	ch := make(chan result, 4)

	// ── Fear & Greed ──────────────────────────────────────────────────────────
	go func() {
		pts, err := s.cryptoClient.FetchFearGreed(90)
		if err != nil {
			ch <- result{key: "fng", err: err}
			return
		}
		var current int
		var cls string
		if len(pts) > 0 {
			last := pts[len(pts)-1]
			current = last.Value
			cls = last.Classification
		}
		status := "GREEN"
		if current < 25 {
			status = "RED"
		} else if current < 45 {
			status = "YELLOW"
		}
		sparkline := make([]crypto.Point, len(pts))
		for i, p := range pts {
			sparkline[i] = crypto.Point{Date: p.Date, Value: float64(p.Value)}
		}
		ch <- result{key: "fng", metrics: []CryptoMetric{{
			ID: "fng", Name: "Fear & Greed Index",
			Value: float64(current), Label: fmt.Sprintf("%d — %s", current, cls),
			Classification: cls, Status: status, Points: sparkline,
		}}}
	}()

	// ── CoinGecko Global → market cap + BTC dominance ─────────────────────────
	go func() {
		g, err := s.cryptoClient.FetchGlobal()
		if err != nil {
			ch <- result{key: "global", err: err}
			return
		}
		mcStatus := "GREEN"
		if g.MarketCap24hChg < -10 {
			mcStatus = "RED"
		} else if g.MarketCap24hChg < -5 {
			mcStatus = "YELLOW"
		}
		domStatus := "GREEN"
		if g.BTCDominance > 65 {
			domStatus = "RED"
		} else if g.BTCDominance > 55 {
			domStatus = "YELLOW"
		}
		ch <- result{key: "global", metrics: []CryptoMetric{
			{
				ID: "market_cap", Name: "Total Crypto Market Cap",
				Value: g.MarketCapUSD / 1e12, Change24h: g.MarketCap24hChg,
				Label:  fmt.Sprintf("$%.2fT (%+.1f%% 24h)", g.MarketCapUSD/1e12, g.MarketCap24hChg),
				Status: mcStatus,
			},
			{
				ID: "btc_dominance", Name: "BTC Market Dominance",
				Value: g.BTCDominance, Label: fmt.Sprintf("%.1f%%", g.BTCDominance),
				Status: domStatus,
			},
		}}
	}()

	// ── CoinGecko BTC price history ───────────────────────────────────────────
	go func() {
		pts, err := s.cryptoClient.FetchBTCOHLC(90)
		if err != nil {
			ch <- result{key: "btc", err: err}
			return
		}
		var current float64
		if len(pts) > 0 {
			current = pts[len(pts)-1].Value
		}
		status := "GREEN"
		if len(pts) >= 8 {
			chg := (pts[len(pts)-1].Value - pts[len(pts)-8].Value) / pts[len(pts)-8].Value * 100
			if chg < -15 {
				status = "RED"
			} else if chg < -7 {
				status = "YELLOW"
			}
		}
		ch <- result{key: "btc", metrics: []CryptoMetric{{
			ID: "btc_price", Name: "Bitcoin Price",
			Value: current, Label: fmt.Sprintf("$%s", formatLargeNum(current)),
			Status: status, Points: pts,
		}}}
	}()

	// ── Blockchain.com Active Addresses (adoption) ────────────────────────────
	go func() {
		pts, err := s.cryptoClient.FetchActiveAddresses("1year")
		if err != nil {
			ch <- result{key: "addrs", err: err}
			return
		}
		var current float64
		if len(pts) > 0 {
			current = pts[len(pts)-1].Value
		}
		status := "GREEN"
		if len(pts) >= 2 {
			yoy := (pts[len(pts)-1].Value - pts[0].Value) / pts[0].Value * 100
			if yoy < -20 {
				status = "RED"
			} else if yoy < -5 {
				status = "YELLOW"
			}
		}
		ch <- result{key: "addrs", metrics: []CryptoMetric{{
			ID: "active_addrs", Name: "BTC Active Addresses",
			Value: current, Label: fmt.Sprintf("%.0fK/day", current/1000),
			Status: status, Points: pts,
		}}}
	}()

	// Collect all 4 goroutine results
	ordered := []string{"fng", "btc", "global", "addrs"}
	bucket := make(map[string][]CryptoMetric)
	for i := 0; i < 4; i++ {
		r := <-ch
		if r.err != nil {
			log.Printf("[CRYPTO] %s error: %v", r.key, r.err)
			continue
		}
		bucket[r.key] = r.metrics
	}

	var out []CryptoMetric
	for _, key := range ordered {
		out = append(out, bucket[key]...)
	}

	return &CryptoResponse{
		Success:   len(out) > 0,
		Metrics:   out,
		FetchedAt: time.Now().Format(time.RFC3339),
	}, nil
}

// formatLargeNum formats a float with comma thousands separator.
func formatLargeNum(f float64) string {
	s := fmt.Sprintf("%d", int64(f))
	result := make([]byte, 0, len(s)+4)
	for i, ch := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, ch)
	}
	return string(result)
}

// evictLoop runs a periodic sweep so the chart cache doesn't hold stale entries forever.
func (s *Server) evictLoop() {
	t := time.NewTicker(s.chartTTL)
	defer t.Stop()
	for range t.C {
		s.chartMu.Lock()
		before := len(s.chartCache)
		s.sweepExpiredCharts()
		after := len(s.chartCache)
		s.chartMu.Unlock()
		if before != after {
			log.Printf("[CACHE] chart eviction: %d → %d entries", before, after)
		}
	}
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
		// Recession signals
		"RECPROUSM156N", "SAHMREALTIME", "ICSA",
		// Financial stress
		"STLFSI4", "NFCI",
		// Yield curve / rates / credit
		"T10Y2Y", "T10Y3MM", "BAA10YM", "T10YIE",
		// Labor
		"UNRATE", "U6RATE",
		// Markets
		"VIXCLS", "SP500",
		// Housing
		"MORTGAGE30US", "CUSR0000SEHA", "RRVRUSQ156N", "CSUSHPISA",
		// Foreclosure / mortgage distress
		"DRSFRMACBS", "RCMFLBBALDPDPCT30P", "RCMFLBBALDPDPCT90P",
		// Consumer credit
		"DROCLACBS", "DRCCLACBS", "CORCACBS", "CORCCACBS",
		// Consumer health
		"PSAVERT", "UMCSENT",
		// Debt & fiscal
		"GFDEGDQ188S",
		// Energy
		"DCOILWTICO", "GASREGCOVW",
		// Broad trade / debt (YoY)
		"DTWEXBGS", "TOTALNS",
	}

	var results []*indicators.IndicatorResult

	for _, s := range series {
		log.Printf("[FRED] Processing series: %s", s)
		switch s {

		// ── Recession signals ─────────────────────────────────────────────
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

		case "SAHMREALTIME":
			if obs, err := client.FetchSeries(s, 1); err == nil && len(obs) > 0 {
				val, _ := fred.ParseFloat(obs[0].Value)
				res, _ := indicators.ScoreSahmDirect("Sahm Rule Real-Time", s, obs[0].Date, val)
				results = append(results, res)
			} else {
				log.Printf("[FRED] %s error: %v", s, err)
			}

		case "ICSA":
			if obs, err := client.FetchSeries(s, 1); err == nil && len(obs) > 0 {
				val, _ := fred.ParseFloat(obs[0].Value)
				res, _ := indicators.ScoreJoblessClaims("Initial Jobless Claims", s, obs[0].Date, val)
				results = append(results, res)
			} else {
				log.Printf("[FRED] %s error: %v", s, err)
			}

		// ── Financial stress ──────────────────────────────────────────────
		case "STLFSI4":
			if obs, err := client.FetchSeries(s, 1); err == nil && len(obs) > 0 {
				val, _ := fred.ParseFloat(obs[0].Value)
				res, _ := indicators.ScoreFinancialStress("St. Louis Financial Stress", s, obs[0].Date, val, 0.0, 1.0)
				results = append(results, res)
			} else {
				log.Printf("[FRED] %s error: %v", s, err)
			}

		case "NFCI":
			if obs, err := client.FetchSeries(s, 1); err == nil && len(obs) > 0 {
				val, _ := fred.ParseFloat(obs[0].Value)
				res, _ := indicators.ScoreFinancialStress("Chicago Fed Fin. Conditions", s, obs[0].Date, val, 0.0, 0.5)
				results = append(results, res)
			} else {
				log.Printf("[FRED] %s error: %v", s, err)
			}

		// ── Yield curve / rates ───────────────────────────────────────────
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
		case "T10YIE":
			if obs, err := client.FetchSeries(s, 1); err == nil && len(obs) > 0 {
				val, _ := fred.ParseFloat(obs[0].Value)
				res, _ := indicators.ScoreBreakevenInflation("10-Yr Breakeven Inflation", s, obs[0].Date, val)
				results = append(results, res)
			} else {
				log.Printf("[FRED] %s error: %v", s, err)
			}

		// ── Labor ─────────────────────────────────────────────────────────
		case "UNRATE":
			if obs, err := client.FetchSeries(s, 24); err == nil {
				if res, err := indicators.ComputeSahm(obs); err == nil {
					results = append(results, res)
				} else {
					log.Printf("[FRED] Sahm failed: %v", err)
				}
			}
		case "U6RATE":
			if obs, err := client.FetchSeries(s, 1); err == nil && len(obs) > 0 {
				val, _ := fred.ParseFloat(obs[0].Value)
				res, _ := indicators.ScoreU6("U-6 Broad Unemployment", s, obs[0].Date, val)
				results = append(results, res)
			} else {
				log.Printf("[FRED] %s error: %v", s, err)
			}

		// ── Markets ───────────────────────────────────────────────────────
		case "VIXCLS":
			if obs, err := client.FetchSeries(s, 1); err == nil && len(obs) > 0 {
				val, _ := fred.ParseFloat(obs[0].Value)
				res, _ := indicators.ScoreVIX("VIX Volatility Index", s, obs[0].Date, val)
				results = append(results, res)
			} else {
				log.Printf("[FRED] %s error: %v", s, err)
			}
		case "SP500":
			// YoY: ~245 trading-day lookback. Request 270 to absorb missing-value gaps.
			if obs, err := client.FetchSeries(s, 270); err == nil {
				if res, err := indicators.ScoreYoYChange("S&P 500 YoY", s, obs, 245, 5.0, 15.0, false); err == nil {
					results = append(results, res)
				} else {
					log.Printf("[FRED] SP500 YoY: %v", err)
				}
			} else {
				log.Printf("[FRED] %s error: %v", s, err)
			}

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
		case "CSUSHPISA":
			// YoY: 12 monthly observations
			if obs, err := client.FetchSeries(s, 14); err == nil {
				if res, err := indicators.ScoreYoYChange("Case-Shiller Home Price YoY", s, obs, 12, 5.0, 10.0, true); err == nil {
					results = append(results, res)
				} else {
					log.Printf("[FRED] CSUSHPISA YoY: %v", err)
				}
			} else {
				log.Printf("[FRED] %s error: %v", s, err)
			}

		// ── Mortgage / foreclosure ────────────────────────────────────────
		case "DRSFRMACBS":
			if obs, err := client.FetchSeries(s, 1); err == nil && len(obs) > 0 {
				val, _ := fred.ParseFloat(obs[0].Value)
				res, _ := indicators.ScoreMortgageDelinquency("Residential Mortgage Delinquency", s, obs[0].Date, val)
				results = append(results, res)
			}
		case "RCMFLBBALDPDPCT30P":
			if obs, err := client.FetchSeries(s, 1); err == nil && len(obs) > 0 {
				val, _ := fred.ParseFloat(obs[0].Value)
				res, _ := indicators.ScoreSeriouslyDelinquent("Mortgage 30+ DPD (Large Banks)", s, obs[0].Date, val, 2.0, 3.5)
				results = append(results, res)
			}
		case "RCMFLBBALDPDPCT90P":
			if obs, err := client.FetchSeries(s, 1); err == nil && len(obs) > 0 {
				val, _ := fred.ParseFloat(obs[0].Value)
				res, _ := indicators.ScoreSeriouslyDelinquent("Mortgage 90+ DPD / Foreclosure", s, obs[0].Date, val, 1.5, 2.5)
				results = append(results, res)
			}

		// ── Consumer credit ───────────────────────────────────────────────
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

		// ── Consumer health ───────────────────────────────────────────────
		case "PSAVERT":
			if obs, err := client.FetchSeries(s, 1); err == nil && len(obs) > 0 {
				val, _ := fred.ParseFloat(obs[0].Value)
				res, _ := indicators.ScoreSavingsRate("Personal Savings Rate", s, obs[0].Date, val)
				results = append(results, res)
			} else {
				log.Printf("[FRED] %s error: %v", s, err)
			}
		case "UMCSENT":
			if obs, err := client.FetchSeries(s, 1); err == nil && len(obs) > 0 {
				val, _ := fred.ParseFloat(obs[0].Value)
				res, _ := indicators.ScoreConsumerSentiment("Consumer Sentiment", s, obs[0].Date, val)
				results = append(results, res)
			} else {
				log.Printf("[FRED] %s error: %v", s, err)
			}

		// ── Debt & fiscal ─────────────────────────────────────────────────
		case "GFDEGDQ188S":
			if obs, err := client.FetchSeries(s, 1); err == nil && len(obs) > 0 {
				val, _ := fred.ParseFloat(obs[0].Value)
				res, _ := indicators.ScoreDebtGDP("Federal Debt / GDP", s, obs[0].Date, val)
				results = append(results, res)
			} else {
				log.Printf("[FRED] %s error: %v", s, err)
			}

		// ── Energy ────────────────────────────────────────────────────────
		case "DCOILWTICO":
			if obs, err := client.FetchSeries(s, 1); err == nil && len(obs) > 0 {
				val, _ := fred.ParseFloat(obs[0].Value)
				res, _ := indicators.ScoreOilPrice("WTI Crude Oil", s, obs[0].Date, val)
				results = append(results, res)
			} else {
				log.Printf("[FRED] %s error: %v", s, err)
			}
		case "GASREGCOVW":
			if obs, err := client.FetchSeries(s, 1); err == nil && len(obs) > 0 {
				val, _ := fred.ParseFloat(obs[0].Value)
				res, _ := indicators.ScoreGasPrice("Regular Gasoline", s, obs[0].Date, val)
				results = append(results, res)
			}

		// ── Trade / total debt (YoY) ──────────────────────────────────────
		case "DTWEXBGS":
			// Dollar index — YoY on ~245 trading-day lookback. Request 270 to absorb missing-value gaps.
			if obs, err := client.FetchSeries(s, 270); err == nil {
				if res, err := indicators.ScoreYoYChange("Dollar Index YoY", s, obs, 245, 5.0, 10.0, true); err == nil {
					results = append(results, res)
				} else {
					log.Printf("[FRED] DTWEXBGS YoY: %v", err)
				}
			} else {
				log.Printf("[FRED] %s error: %v", s, err)
			}
		case "TOTALNS":
			// Total consumer credit outstanding — YoY on 12 monthly obs
			if obs, err := client.FetchSeries(s, 14); err == nil {
				if res, err := indicators.ScoreYoYChange("Total Consumer Credit YoY", s, obs, 12, 4.0, 8.0, true); err == nil {
					results = append(results, res)
				} else {
					log.Printf("[FRED] TOTALNS YoY: %v", err)
				}
			} else {
				log.Printf("[FRED] %s error: %v", s, err)
			}
		}
	}

	return results, nil
}
