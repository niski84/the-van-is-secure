package fred

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type Observation struct {
	Date  string `json:"date"`
	Value string `json:"value"`
}

type SeriesResponse struct {
	Observations []Observation `json:"observations"`
}

type Client struct {
	APIKey  string
	BaseURL string
	HTTP    *http.Client
}

func NewClient(apiKey string, timeout time.Duration) (*Client, error) {
	log.Printf("Creating new FRED client with timeout %v", timeout)
	return &Client{
		APIKey:  apiKey,
		BaseURL: "https://api.stlouisfed.org/fred/series/observations",
		HTTP: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

func (c *Client) FetchSeries(seriesID string, limit int) ([]Observation, error) {
	log.Printf("Fetching series %s with limit %d", seriesID, limit)

	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse base URL: %w", err)
	}

	q := u.Query()
	q.Set("series_id", seriesID)
	q.Set("api_key", c.APIKey)
	q.Set("file_type", "json")
	q.Set("sort_order", "desc")
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	u.RawQuery = q.Encode()

	resp, err := c.HTTP.Get(u.String())
	if err != nil {
		return nil, fmt.Errorf("failed to fetch series %s: %w", seriesID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("FRED API returned status %d for series %s", resp.StatusCode, seriesID)
	}

	var res SeriesResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("failed to decode FRED response for %s: %w", seriesID, err)
	}

	return res.Observations, nil
}

func ParseFloat(s string) (float64, error) {
	if s == "." {
		return 0, fmt.Errorf("value is missing (.)")
	}
	return strconv.ParseFloat(s, 64)
}

