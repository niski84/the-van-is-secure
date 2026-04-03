// Package cdc fetches public health data from data.cdc.gov (Socrata API).
// No API key or signup required — all endpoints are open access.
// Rate limit: ~1000 unauthenticated requests/hour per IP (well within our usage).
package cdc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Client fetches CDC open data.
type Client struct {
	HTTP *http.Client
}

func NewClient(timeout time.Duration) *Client {
	return &Client{HTTP: &http.Client{Timeout: timeout}}
}

// Point is a (date, value) pair for sparklines.
type Point struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"`
}

var monthNum = map[string]int{
	"january": 1, "february": 2, "march": 3, "april": 4,
	"may": 5, "june": 6, "july": 7, "august": 8,
	"september": 9, "october": 10, "november": 11, "december": 12,
}

// GetOverdoseDeaths returns monthly 12-month-rolling national drug overdose death
// totals, oldest-first. Returns points and the most recent 12-month total.
// Dataset: NCHS Provisional Drug Overdose Death Counts (xkb8-kh2a).
// Lag: ~4 months. Uses predicted_value (corrects for reporting delay) when available.
func (c *Client) GetOverdoseDeaths(limit int) (pts []Point, latest float64, err error) {
	// state_name="United States" is the national aggregate row.
	// period="12 month-ending" rows give rolling 12-month totals (smoothed, comparable YoY).
	reqURL := "https://data.cdc.gov/resource/xkb8-kh2a.json?" +
		"state_name=United+States" +
		"&indicator=Number+of+Drug+Overdose+Deaths" +
		"&$limit=500"

	resp, err := c.HTTP.Get(reqURL)
	if err != nil {
		return nil, 0, fmt.Errorf("cdc overdose fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("cdc overdose: HTTP %d", resp.StatusCode)
	}

	var rows []struct {
		Year           string `json:"year"`
		Month          string `json:"month"`
		Period         string `json:"period"`
		DataValue      string `json:"data_value"`
		PredictedValue string `json:"predicted_value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return nil, 0, fmt.Errorf("cdc overdose decode: %w", err)
	}

	type dp struct {
		year, month int
		value       float64
	}
	var collected []dp

	for _, row := range rows {
		// Only keep 12-month rolling rows
		if !strings.Contains(strings.ToLower(row.Period), "12 month") {
			continue
		}
		yr, err := strconv.Atoi(strings.TrimSpace(row.Year))
		if err != nil {
			continue
		}
		mo := monthNum[strings.ToLower(strings.TrimSpace(row.Month))]
		if mo == 0 {
			continue
		}

		// Prefer predicted_value (corrects for reporting lag), fall back to data_value.
		var val float64
		if row.PredictedValue != "" {
			if v, e := strconv.ParseFloat(strings.TrimSpace(row.PredictedValue), 64); e == nil && v > 0 {
				val = v
			}
		}
		if val == 0 && row.DataValue != "" {
			if v, e := strconv.ParseFloat(strings.TrimSpace(row.DataValue), 64); e == nil && v > 0 {
				val = v
			}
		}
		if val == 0 {
			continue // suppressed or missing
		}

		collected = append(collected, dp{yr, mo, val})
	}

	if len(collected) == 0 {
		return nil, 0, fmt.Errorf("cdc overdose: no data returned")
	}

	// Sort chronological, deduplicate (keep latest value for each year/month).
	sort.Slice(collected, func(i, j int) bool {
		if collected[i].year != collected[j].year {
			return collected[i].year < collected[j].year
		}
		return collected[i].month < collected[j].month
	})

	seen := map[string]bool{}
	var unique []dp
	for _, d := range collected {
		k := fmt.Sprintf("%d-%02d", d.year, d.month)
		if !seen[k] {
			seen[k] = true
			unique = append(unique, d)
		}
	}

	if limit > 0 && len(unique) > limit {
		unique = unique[len(unique)-limit:]
	}

	pts = make([]Point, len(unique))
	for i, p := range unique {
		pts[i] = Point{
			Date:  fmt.Sprintf("%d-%02d", p.year, p.month),
			Value: p.value,
		}
	}
	if len(pts) > 0 {
		latest = pts[len(pts)-1].Value
	}
	return pts, latest, nil
}

// GetAnxietyPrevalence returns biweekly national % of adults reporting anxiety
// or depression symptoms from the CDC Household Pulse Survey, oldest-first.
// Dataset: 8pt5-q6wp. Survey ran Apr 2020 – Sep 2024; data is static after that.
func (c *Client) GetAnxietyPrevalence(limit int) (pts []Point, latest float64, err error) {
	// "group" is a SoQL reserved word — must use $where with backtick escaping.
	reqURL := "https://data.cdc.gov/resource/8pt5-q6wp.json?" +
		"$where=%60group%60=%27National+Estimate%27" +
		"+AND+indicator=%27Symptoms+of+Anxiety+Disorder+or+Depressive+Disorder%27" +
		"+AND+state=%27United+States%27" +
		"&$limit=500" +
		"&$order=time_period_end_date+ASC"

	resp, err := c.HTTP.Get(reqURL)
	if err != nil {
		return nil, 0, fmt.Errorf("cdc pulse fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("cdc pulse: HTTP %d", resp.StatusCode)
	}

	var rows []struct {
		EndDate string `json:"time_period_end_date"` // "2024-09-16T00:00:00.000"
		Value   string `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return nil, 0, fmt.Errorf("cdc pulse decode: %w", err)
	}

	for _, row := range rows {
		v, err := strconv.ParseFloat(strings.TrimSpace(row.Value), 64)
		if err != nil || v == 0 {
			continue
		}
		date := row.EndDate
		if len(date) >= 10 {
			date = date[:10] // trim to YYYY-MM-DD
		}
		pts = append(pts, Point{Date: date, Value: v})
	}

	if len(pts) == 0 {
		return nil, 0, fmt.Errorf("cdc pulse: no data returned")
	}

	if limit > 0 && len(pts) > limit {
		pts = pts[len(pts)-limit:]
	}

	latest = pts[len(pts)-1].Value
	return pts, latest, nil
}
