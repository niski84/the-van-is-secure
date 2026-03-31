package crypto

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Point is a generic (date, value) pair used for all sparklines.
type Point struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"`
}

// GlobalData holds the CoinGecko /global snapshot.
type GlobalData struct {
	MarketCapUSD    float64 `json:"market_cap_usd"`
	MarketCap24hChg float64 `json:"market_cap_24h_chg"` // %
	BTCDominance    float64 `json:"btc_dominance"`       // %
	VolumeUSD       float64 `json:"volume_usd"`
}

// FearGreed is a single day's Fear & Greed reading.
type FearGreed struct {
	Date           string `json:"date"`
	Value          int    `json:"value"`
	Classification string `json:"classification"`
}

// Client calls the three keyless crypto APIs.
type Client struct {
	HTTP *http.Client
}

func NewClient(timeout time.Duration) *Client {
	return &Client{HTTP: &http.Client{Timeout: timeout}}
}

// ── CoinGecko ─────────────────────────────────────────────────────────────────

func (c *Client) FetchGlobal() (GlobalData, error) {
	resp, err := c.HTTP.Get("https://api.coingecko.com/api/v3/global")
	if err != nil {
		return GlobalData{}, fmt.Errorf("coingecko global: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return GlobalData{}, fmt.Errorf("coingecko global: HTTP %d", resp.StatusCode)
	}

	var raw struct {
		Data struct {
			TotalMarketCap map[string]float64 `json:"total_market_cap"`
			TotalVolume    map[string]float64 `json:"total_volume"`
			MarketCapPct   map[string]float64 `json:"market_cap_percentage"`
			MarketCapChg   float64            `json:"market_cap_change_percentage_24h_usd"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return GlobalData{}, fmt.Errorf("coingecko global decode: %w", err)
	}
	return GlobalData{
		MarketCapUSD:    raw.Data.TotalMarketCap["usd"],
		MarketCap24hChg: raw.Data.MarketCapChg,
		BTCDominance:    raw.Data.MarketCapPct["btc"],
		VolumeUSD:       raw.Data.TotalVolume["usd"],
	}, nil
}

// FetchBTCOHLC returns daily close prices for the last `days` days.
// CoinGecko OHLC array: [timestamp_ms, open, high, low, close]
func (c *Client) FetchBTCOHLC(days int) ([]Point, error) {
	url := fmt.Sprintf("https://api.coingecko.com/api/v3/coins/bitcoin/ohlc?vs_currency=usd&days=%d", days)
	resp, err := c.HTTP.Get(url)
	if err != nil {
		return nil, fmt.Errorf("coingecko ohlc: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("coingecko ohlc: HTTP %d", resp.StatusCode)
	}

	var raw [][]float64
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("coingecko ohlc decode: %w", err)
	}

	seen := map[string]bool{}
	pts := make([]Point, 0, len(raw))
	for _, row := range raw {
		if len(row) < 5 {
			continue
		}
		ts := time.UnixMilli(int64(row[0])).UTC()
		date := ts.Format("2006-01-02")
		if seen[date] {
			continue // keep first candle per day
		}
		seen[date] = true
		pts = append(pts, Point{Date: date, Value: row[4]}) // close price
	}
	return pts, nil
}

// ── Alternative.me Fear & Greed ───────────────────────────────────────────────

func (c *Client) FetchFearGreed(days int) ([]FearGreed, error) {
	url := fmt.Sprintf("https://api.alternative.me/fng/?limit=%d&format=json", days)
	resp, err := c.HTTP.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fng fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fng: HTTP %d", resp.StatusCode)
	}

	var raw struct {
		Data []struct {
			Value              string `json:"value"`
			ValueClassification string `json:"value_classification"`
			Timestamp          string `json:"timestamp"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("fng decode: %w", err)
	}

	pts := make([]FearGreed, 0, len(raw.Data))
	for _, d := range raw.Data {
		var v int
		fmt.Sscanf(d.Value, "%d", &v)
		var ts int64
		fmt.Sscanf(d.Timestamp, "%d", &ts)
		date := time.Unix(ts, 0).UTC().Format("2006-01-02")
		pts = append(pts, FearGreed{Date: date, Value: v, Classification: d.ValueClassification})
	}
	// raw is desc (newest first); reverse to asc for charts
	for i, j := 0, len(pts)-1; i < j; i, j = i+1, j-1 {
		pts[i], pts[j] = pts[j], pts[i]
	}
	return pts, nil
}

// ── Blockchain.com Active Addresses ──────────────────────────────────────────

// FetchActiveAddresses returns daily unique Bitcoin address counts.
// timespan: "30days", "180days", "1year", "all"
func (c *Client) FetchActiveAddresses(timespan string) ([]Point, error) {
	url := fmt.Sprintf("https://api.blockchain.info/charts/n-unique-addresses?timespan=%s&format=json&sampled=true", timespan)
	resp, err := c.HTTP.Get(url)
	if err != nil {
		return nil, fmt.Errorf("blockchain addrs: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("blockchain addrs: HTTP %d", resp.StatusCode)
	}

	var raw struct {
		Values []struct {
			X int64   `json:"x"` // unix timestamp
			Y float64 `json:"y"` // address count
		} `json:"values"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("blockchain addrs decode: %w", err)
	}

	pts := make([]Point, 0, len(raw.Values))
	for _, v := range raw.Values {
		pts = append(pts, Point{
			Date:  time.Unix(v.X, 0).UTC().Format("2006-01-02"),
			Value: v.Y,
		})
	}
	return pts, nil
}
