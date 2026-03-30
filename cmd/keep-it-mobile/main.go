package main

import (
	"embed"
	"fmt"
	"io/fs"
	"keep-it-mobile/internal/feeds"
	"keep-it-mobile/internal/fred"
	"keep-it-mobile/internal/imgcache"
	"keep-it-mobile/internal/server"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

//go:embed web
var webFS embed.FS

func main() {
	apiKey := os.Getenv("FRED_API_KEY")
	if apiKey == "" {
		log.Fatal("FRED_API_KEY environment variable must be set")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fredClient, err := fred.NewClient(apiKey, 10*time.Second)
	if err != nil {
		log.Fatalf("Failed to create FRED client: %v", err)
	}

	feedFetcher := feeds.NewFetcher(feeds.DefaultSources)

	// Image disk cache — defaults to $TMPDIR/van-img-cache, overridable via IMG_CACHE_DIR
	cacheDir := os.Getenv("IMG_CACHE_DIR")
	if cacheDir == "" {
		cacheDir = filepath.Join(os.TempDir(), "van-img-cache")
	}
	ic, err := imgcache.New(cacheDir)
	if err != nil {
		log.Fatalf("Failed to create image cache at %s: %v", cacheDir, err)
	}
	log.Printf("[IMGCACHE] disk cache at %s", cacheDir)

	// Cleanup: run once at startup, then every 2 hours
	// Keeps files ≤ 7 days old and caps the directory at 400 images
	go func() {
		ic.Cleanup(7*24*time.Hour, 400)
		t := time.NewTicker(2 * time.Hour)
		defer t.Stop()
		for range t.C {
			ic.Cleanup(7*24*time.Hour, 400)
		}
	}()

	srv := server.NewServer(fredClient, feedFetcher, ic)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", server.HandleHealth)
	mux.HandleFunc("/api/indicators", srv.HandleIndicators)
	mux.HandleFunc("/api/articles", srv.HandleArticles)
	mux.HandleFunc("/api/chart", srv.HandleChart)
	mux.HandleFunc("/api/article-image", srv.HandleArticleImage)
	mux.HandleFunc("/img/cache/", srv.HandleCachedImage)

	webRoot, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("Failed to setup web filesystem: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(webRoot)))

	addr := fmt.Sprintf(":%s", port)
	log.Printf("[SERVER] The Van Is Secure — listening on http://localhost%s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("[SERVER] Failed: %v", err)
	}
}
