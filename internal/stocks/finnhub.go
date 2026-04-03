package stocks

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Point is a (date, value) pair for sparklines.
type Point struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"`
}

// Quote holds the latest price snapshot from Finnhub.
type Quote struct {
	Current   float64
	ChangePct float64 // % change from previous close
	PrevClose float64
}

// Client calls the Finnhub stock API.
type Client struct {
	APIKey string
	HTTP   *http.Client
}

func NewClient(apiKey string, timeout time.Duration) *Client {
	return &Client{APIKey: apiKey, HTTP: &http.Client{Timeout: timeout}}
}

// GetQuote fetches the latest price and daily % change for a symbol.
func (c *Client) GetQuote(symbol string) (Quote, error) {
	url := fmt.Sprintf("https://finnhub.io/api/v1/quote?symbol=%s&token=%s", symbol, c.APIKey)
	resp, err := c.HTTP.Get(url)
	if err != nil {
		return Quote{}, fmt.Errorf("finnhub quote %s: %w", symbol, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Quote{}, fmt.Errorf("finnhub quote %s: HTTP %d", symbol, resp.StatusCode)
	}
	var raw struct {
		C  float64 `json:"c"`  // current price
		DP float64 `json:"dp"` // % change from prev close
		PC float64 `json:"pc"` // previous close
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return Quote{}, fmt.Errorf("finnhub quote %s decode: %w", symbol, err)
	}
	if raw.C == 0 {
		return Quote{}, fmt.Errorf("finnhub quote %s: zero price (market closed or bad key)", symbol)
	}
	return Quote{Current: raw.C, ChangePct: raw.DP, PrevClose: raw.PC}, nil
}

// GetWeeklyCandles returns weekly close prices for the past `weeks` weeks,
// oldest-first. Also returns the 52-week high and low.
func (c *Client) GetWeeklyCandles(symbol string, weeks int) (pts []Point, high52w, low52w float64, err error) {
	to := time.Now().Unix()
	from := time.Now().AddDate(0, 0, -(weeks*7 + 14)).Unix() // buffer for partial weeks
	url := fmt.Sprintf(
		"https://finnhub.io/api/v1/stock/candle?symbol=%s&resolution=W&from=%d&to=%d&token=%s",
		symbol, from, to, c.APIKey,
	)
	resp, err := c.HTTP.Get(url)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("finnhub candle %s: %w", symbol, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, 0, fmt.Errorf("finnhub candle %s: HTTP %d", symbol, resp.StatusCode)
	}
	var raw struct {
		C []float64 `json:"c"` // close
		H []float64 `json:"h"` // high
		L []float64 `json:"l"` // low
		T []int64   `json:"t"` // unix timestamps
		S string    `json:"s"` // status
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, 0, 0, fmt.Errorf("finnhub candle %s decode: %w", symbol, err)
	}
	if raw.S != "ok" || len(raw.C) == 0 {
		return nil, 0, 0, fmt.Errorf("finnhub candle %s: status=%s, len=%d", symbol, raw.S, len(raw.C))
	}

	pts = make([]Point, 0, len(raw.C))
	high52w = raw.H[0]
	low52w = raw.L[0]
	for i, close := range raw.C {
		ts := time.Unix(raw.T[i], 0).UTC().Format("2006-01-02")
		pts = append(pts, Point{Date: ts, Value: close})
		if raw.H[i] > high52w {
			high52w = raw.H[i]
		}
		if raw.L[i] < low52w {
			low52w = raw.L[i]
		}
	}
	return pts, high52w, low52w, nil
}
