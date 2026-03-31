package fred

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	maxRetries = 3
	retryBase  = 2 * time.Second
	retryMax   = 16 * time.Second
)

type Observation struct {
	Date  string `json:"date"`
	Value string `json:"value"`
}

type SeriesResponse struct {
	Observations []Observation `json:"observations"`
}

// fredError carries the HTTP status so callers can decide whether to retry.
type fredError struct {
	StatusCode int
	SeriesID   string
}

func (e *fredError) Error() string {
	return fmt.Sprintf("FRED API returned status %d for series %s", e.StatusCode, e.SeriesID)
}

type Client struct {
	APIKey  string
	BaseURL string
	HTTP    *http.Client
	// sem limits the number of simultaneous outbound FRED HTTP requests.
	// FRED's public API has no published rate-limit but fires 500s under burst load.
	sem chan struct{}
}

func NewClient(apiKey string, timeout time.Duration) (*Client, error) {
	return &Client{
		APIKey:  apiKey,
		BaseURL: "https://api.stlouisfed.org/fred/series/observations",
		HTTP:    &http.Client{Timeout: timeout},
		sem:     make(chan struct{}, 3), // max 3 simultaneous FRED calls
	}, nil
}

// FetchSeries fetches observations with exponential back-off retry on 5xx errors.
// A concurrency semaphore ensures at most 3 calls are in-flight at any time.
func (c *Client) FetchSeries(seriesID string, limit int) ([]Observation, error) {
	c.sem <- struct{}{}
	defer func() { <-c.sem }()

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(math.Pow(2, float64(attempt-1))) * retryBase
			if delay > retryMax {
				delay = retryMax
			}
			log.Printf("[FRED] %s: retry %d/%d in %v (last: %v)", seriesID, attempt, maxRetries, delay, lastErr)
			time.Sleep(delay)
		}

		obs, err := c.fetchOnce(seriesID, limit)
		if err == nil {
			if attempt > 0 {
				log.Printf("[FRED] %s: recovered on attempt %d", seriesID, attempt+1)
			}
			return obs, nil
		}
		lastErr = err

		// Only retry server errors (5xx). Client errors (4xx), parse failures,
		// and network timeouts are not retried — they won't improve with time.
		var fe *fredError
		if !errors.As(err, &fe) || fe.StatusCode < 500 {
			return nil, err
		}
	}
	return nil, fmt.Errorf("FRED: %s failed after %d retries: %w", seriesID, maxRetries, lastErr)
}

func (c *Client) fetchOnce(seriesID string, limit int) ([]Observation, error) {
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
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
		return nil, fmt.Errorf("fetch %s: %w", seriesID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &fredError{StatusCode: resp.StatusCode, SeriesID: seriesID}
	}

	var res SeriesResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("decode %s: %w", seriesID, err)
	}
	return res.Observations, nil
}

func ParseFloat(s string) (float64, error) {
	if s == "." {
		return 0, fmt.Errorf("value is missing (.)")
	}
	return strconv.ParseFloat(s, 64)
}
