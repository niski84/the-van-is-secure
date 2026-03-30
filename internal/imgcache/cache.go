// Package imgcache downloads remote images to a local directory and serves them.
// Keys are the MD5 hex of the source URL, so the same source is never fetched twice.
// A background Cleanup call prunes files older than maxAge or beyond a file-count cap.
package imgcache

import (
	"crypto/md5"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Cache is a disk-backed image cache.
type Cache struct {
	dir      string
	client   *http.Client
	mu       sync.Mutex
	inFlight map[string]bool // keys currently being downloaded
}

// New creates (or opens) a cache rooted at dir.
func New(dir string) (*Cache, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("imgcache: mkdir %s: %w", dir, err)
	}
	return &Cache{
		dir:      dir,
		client:   &http.Client{Timeout: 12 * time.Second},
		inFlight: make(map[string]bool),
	}, nil
}

// Dir returns the cache directory.
func (c *Cache) Dir() string { return c.dir }

// Key returns the 32-char hex cache key for a source URL.
func Key(sourceURL string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(sourceURL)))
}

// Path returns the full filesystem path for a cache key.
func (c *Cache) Path(key string) string {
	return filepath.Join(c.dir, key)
}

// Get returns the cache key for sourceURL, downloading and saving if not yet cached.
// Returns ("", false) if the URL is empty, download fails, or the response is not an image.
func (c *Cache) Get(sourceURL string) (key string, ok bool) {
	if sourceURL == "" {
		return "", false
	}
	key = Key(sourceURL)
	path := filepath.Join(c.dir, key)

	// Fast path: already on disk
	if _, err := os.Stat(path); err == nil {
		return key, true
	}

	// Deduplicate concurrent downloads for the same key
	c.mu.Lock()
	if c.inFlight[key] {
		c.mu.Unlock()
		return "", false
	}
	c.inFlight[key] = true
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.inFlight, key)
		c.mu.Unlock()
	}()

	log.Printf("[IMGCACHE] downloading %s", sourceURL)
	resp, err := c.client.Get(sourceURL)
	if err != nil {
		log.Printf("[IMGCACHE] download error %s: %v", sourceURL, err)
		return "", false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[IMGCACHE] download status %d for %s", resp.StatusCode, sourceURL)
		return "", false
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 6<<20)) // 6 MB cap
	if err != nil {
		log.Printf("[IMGCACHE] read error %s: %v", sourceURL, err)
		return "", false
	}

	// Reject non-image responses (e.g. HTML error pages)
	if ct := http.DetectContentType(data); !strings.HasPrefix(ct, "image/") {
		log.Printf("[IMGCACHE] not an image (%s) for %s", ct, sourceURL)
		return "", false
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		log.Printf("[IMGCACHE] write error %s: %v", path, err)
		return "", false
	}
	log.Printf("[IMGCACHE] cached %s → %s (%d KB)", sourceURL, key[:8]+"…", len(data)/1024)
	return key, true
}

// Cleanup deletes files older than maxAge, then trims to maxFiles keeping the newest.
func (c *Cache) Cleanup(maxAge time.Duration, maxFiles int) {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		log.Printf("[IMGCACHE] cleanup ReadDir: %v", err)
		return
	}

	type fe struct {
		path    string
		modTime time.Time
	}
	var kept []fe
	expired := 0
	now := time.Now()

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		p := filepath.Join(c.dir, e.Name())
		if now.Sub(info.ModTime()) > maxAge {
			os.Remove(p)
			expired++
			continue
		}
		kept = append(kept, fe{p, info.ModTime()})
	}
	if expired > 0 {
		log.Printf("[IMGCACHE] cleanup: pruned %d expired files", expired)
	}

	if len(kept) <= maxFiles {
		return
	}

	// Sort oldest-first, remove excess
	sort.Slice(kept, func(i, j int) bool { return kept[i].modTime.Before(kept[j].modTime) })
	excess := kept[:len(kept)-maxFiles]
	for _, f := range excess {
		os.Remove(f.path)
	}
	log.Printf("[IMGCACHE] cleanup: trimmed %d over-limit files (cap=%d)", len(excess), maxFiles)
}
