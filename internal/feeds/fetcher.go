package feeds

import (
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Article is a normalized article from any RSS/Atom feed.
type Article struct {
	Title    string `json:"title"`
	URL      string `json:"url"`
	Summary  string `json:"summary"`
	Source   string `json:"source"`
	PubDate  string `json:"pub_date"`  // RFC3339
	PubLabel string `json:"pub_label"` // human-readable
	ImageURL string `json:"image_url,omitempty"`
}

// Source defines a feed to ingest.
// If Keywords is non-empty, only articles whose title+summary contain at least
// one keyword (case-insensitive) are kept. Empty Keywords = include everything.
type Source struct {
	Name     string
	URL      string
	Keywords []string
}

// doomKeywords signal acute economic stress — used for broad general-interest
// sources (Guardian, AP) where we want only crisis-level articles.
var doomKeywords = []string{
	"recession", "crisis", "crash", "collapse", "stagflation", "deflation",
	"default", "bankruptcy", "insolvency", "foreclosure", "eviction",
	"unemployment", "layoffs", "layoff", "job cuts", "downturn", "contraction",
	"delinquency", "charge-off", "distress", "contagion", "bubble",
	"bear market", "sell-off", "selloff", "correction", "shock",
	"deficit", "debt ceiling", "sovereign debt", "credit downgrade", "downgrade",
	"negative outlook", "rating cut", "junk", "high yield",
	"tariff", "trade war", "sanctions", "bank failure", "bank run",
	"yield curve", "inversion", "rate hike", "rate cut", "federal reserve",
	"inflation", "cpi", "pce", "consumer price", "housing crash",
	"mortgage", "liquidity", "credit crunch", "bank stress",
}

// economicsKeywords is broader — for editorially-curated economic sources
// (e.g. Naked Capitalism) where general economic language is doom-relevant.
var economicsKeywords = append(append([]string{},
	"economic", "financial", "banking", "debt", "market", "trade",
	"currency", "austerity", "spending cut", "bond", "rate", "gdp",
	"budget", "fiscal", "monetary", "imf", "world bank", "federal reserve",
	"interest rate", "inflation", "growth", "contraction",
), doomKeywords...)

// DefaultSources are free public feeds — no API key required.
var DefaultSources = []Source{
	{Name: "Wolf Street",        URL: "https://wolfstreet.com/feed/"},
	{Name: "Calculated Risk",    URL: "https://www.calculatedriskblog.com/feeds/posts/default"},
	{Name: "Federal Reserve",    URL: "https://www.federalreserve.gov/feeds/press_all.xml"},
	{Name: "MarketWatch",        URL: "https://feeds.marketwatch.com/marketwatch/topstories/"},
	{Name: "Guardian Economics", URL: "https://www.theguardian.com/business/economics/rss",   Keywords: doomKeywords},
	{Name: "Naked Capitalism",   URL: "https://www.nakedcapitalism.com/feed",        Keywords: economicsKeywords},
	{Name: "BBC Business",        URL: "https://feeds.bbci.co.uk/news/business/rss.xml", Keywords: doomKeywords},
}

// package-level regexes for image extraction
var (
	imgSrcRe = regexp.MustCompile(`(?i)<img[^>]+src=["']([^"']+)["']`)

	// og:image / twitter:image — match the complete <meta> tag so we never
	// accidentally pick up content= from an adjacent tag.  The property value
	// must be exactly "og:image" or "twitter:image" (not og:image:width etc.).
	// We support both attribute orderings (property-first and content-first).
	metaOgFwd  = regexp.MustCompile(`(?i)<meta[^>]+?(?:property|name)=["']og:image["'][^>]*?content=["']([^"']{8,})["']`)
	metaOgRev  = regexp.MustCompile(`(?i)<meta[^>]+?content=["']([^"']{8,})["'][^>]*?(?:property|name)=["']og:image["']`)
	metaTwFwd  = regexp.MustCompile(`(?i)<meta[^>]+?(?:property|name)=["']twitter:image["'][^>]*?content=["']([^"']{8,})["']`)
	metaTwRev  = regexp.MustCompile(`(?i)<meta[^>]+?content=["']([^"']{8,})["'][^>]*?(?:property|name)=["']twitter:image["']`)
)

// Fetcher aggregates articles from multiple feeds with a shared cache.
type Fetcher struct {
	client    *http.Client // RSS feed fetches (15s timeout)
	imgClient *http.Client // og:image page fetches (7s timeout)
	sources   []Source
	mu        sync.Mutex
	cache     []Article
	cachedAt  time.Time
	cacheTTL  time.Duration
	imgMu     sync.Mutex
	imgCache  map[string]string // article URL → image URL ("" = tried, none found)
}

// NewFetcher creates a Fetcher with the given sources.
func NewFetcher(sources []Source) *Fetcher {
	return &Fetcher{
		client:    &http.Client{Timeout: 15 * time.Second},
		imgClient: &http.Client{Timeout: 7 * time.Second},
		sources:   sources,
		cacheTTL:  15 * time.Minute,
		imgCache:  make(map[string]string),
	}
}

// Articles returns merged, deduplicated, sorted articles with available images applied.
func (f *Fetcher) Articles() ([]Article, time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.cache) > 0 && time.Since(f.cachedAt) < f.cacheTTL {
		log.Printf("[FEEDS] Cache hit: %d articles", len(f.cache))
		return f.cache, f.cachedAt, nil
	}

	articles := f.fetchAll()
	f.applyImgCache(articles)

	// Background enrichment for articles without images
	toEnrich := make([]int, 0)
	for i, a := range articles {
		if a.ImageURL == "" {
			f.imgMu.Lock()
			_, tried := f.imgCache[a.URL]
			f.imgMu.Unlock()
			if !tried {
				toEnrich = append(toEnrich, i)
			}
		}
	}
	if len(toEnrich) > 0 {
		go f.enrichImages(articles, toEnrich)
	}

	f.cache = articles
	f.cachedAt = time.Now()
	log.Printf("[FEEDS] Refreshed: %d articles (%d queued for image enrichment)", len(f.cache), len(toEnrich))
	return f.cache, f.cachedAt, nil
}

// GetImage returns the cached image URL for an article, fetching it synchronously if needed.
// Called by the /api/article-image endpoint.
func (f *Fetcher) GetImage(articleURL string) string {
	f.imgMu.Lock()
	if img, ok := f.imgCache[articleURL]; ok {
		f.imgMu.Unlock()
		return img
	}
	f.imgMu.Unlock()

	img := f.fetchPageImage(articleURL)

	f.imgMu.Lock()
	f.imgCache[articleURL] = img
	f.imgMu.Unlock()

	// Also update the in-memory article cache if it's there
	f.mu.Lock()
	if img != "" {
		for i := range f.cache {
			if f.cache[i].URL == articleURL {
				f.cache[i].ImageURL = img
				break
			}
		}
	}
	f.mu.Unlock()

	return img
}

func (f *Fetcher) applyImgCache(articles []Article) {
	f.imgMu.Lock()
	defer f.imgMu.Unlock()
	for i := range articles {
		if articles[i].ImageURL == "" {
			if img, ok := f.imgCache[articles[i].URL]; ok && img != "" {
				articles[i].ImageURL = img
			}
		}
	}
}

func (f *Fetcher) enrichImages(articles []Article, indices []int) {
	sem := make(chan struct{}, 5)
	var wg sync.WaitGroup
	for _, idx := range indices {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			img := f.fetchPageImage(articles[i].URL)
			f.imgMu.Lock()
			f.imgCache[articles[i].URL] = img
			if img != "" {
				articles[i].ImageURL = img
			}
			f.imgMu.Unlock()
		}(idx)
	}
	wg.Wait()

	// Update the live cache too
	f.mu.Lock()
	f.applyImgCache(f.cache)
	f.mu.Unlock()

	log.Printf("[FEEDS] Image enrichment done: %d articles processed", len(indices))
}

// fetchPageImage fetches an article page and extracts the best available image URL.
func (f *Fetcher) fetchPageImage(articleURL string) string {
	req, err := http.NewRequest(http.MethodGet, articleURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; TheVanIsSecure/1.0)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := f.imgClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return ""
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	return extractPageImage(string(body), articleURL)
}

// extractPageImage tries multiple strategies to find the best image URL in page HTML.
func extractPageImage(html, articleURL string) string {
	// Strategy 1: og:image / twitter:image meta tags.
	// Use regexes that match the complete <meta> tag to avoid picking up
	// content= values from adjacent tags (e.g. og:image:width → og:url).
	for _, re := range []*regexp.Regexp{metaOgFwd, metaOgRev, metaTwFwd, metaTwRev} {
		if m := re.FindStringSubmatch(html); len(m) > 1 {
			u := strings.TrimSpace(m[1])
			if isHTTP(u) {
				return u
			}
		}
	}

	// Strategy 2: first content <img> (not logo/icon/ui elements)
	parsedBase, _ := url.Parse(articleURL)
	matches := imgSrcRe.FindAllStringSubmatch(html, 40)
	for _, m := range matches {
		src := m[1]
		if !isHTTP(src) {
			continue
		}
		if isUIImage(src) {
			continue
		}
		// Prefer same-domain images in content paths (/uploads/, /images/, /content/, /wp-content/)
		if parsedBase != nil {
			if pu, _ := url.Parse(src); pu != nil && pu.Host == parsedBase.Host {
				p := strings.ToLower(pu.Path)
				if strings.Contains(p, "/uploads/") || strings.Contains(p, "/images/") ||
					strings.Contains(p, "/content/") || strings.Contains(p, "wp-content") {
					return src
				}
			}
		}
	}

	// Strategy 3: any same-domain image that isn't a UI element
	for _, m := range matches {
		src := m[1]
		if !isHTTP(src) || isUIImage(src) {
			continue
		}
		if parsedBase != nil {
			if pu, _ := url.Parse(src); pu != nil && pu.Host == parsedBase.Host {
				return src
			}
		}
	}

	return ""
}

func isHTTP(u string) bool { return strings.HasPrefix(u, "http") }

// matchesKeywords returns true if keywords is empty (no filter) or if
// title+summary contains at least one keyword (case-insensitive).
func matchesKeywords(title, summary string, keywords []string) bool {
	if len(keywords) == 0 {
		return true
	}
	haystack := strings.ToLower(title + " " + summary)
	for _, kw := range keywords {
		if strings.Contains(haystack, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

func isUIImage(src string) bool {
	lower := strings.ToLower(src)
	for _, s := range []string{"logo", "avatar", "gravatar", "favicon", "sprite", "pixel", "1x1", "blank", "placeholder", "icon-", "badge", "button", "arrow", "close", "menu", "header", "footer"} {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

// ── Feed fetching ─────────────────────────────────────────────────────────────

func (f *Fetcher) fetchAll() []Article {
	type result struct{ articles []Article }
	ch := make(chan result, len(f.sources))
	for _, src := range f.sources {
		go func(s Source) {
			arts, err := f.fetchSource(s)
			if err != nil {
				log.Printf("[FEEDS] %s: %v", s.Name, err)
				ch <- result{nil}
				return
			}
			log.Printf("[FEEDS] %s: %d articles", s.Name, len(arts))
			ch <- result{arts}
		}(src)
	}

	var all []Article
	seen := make(map[string]bool)
	for range f.sources {
		r := <-ch
		for _, a := range r.articles {
			if a.URL == "" || seen[a.URL] {
				continue
			}
			seen[a.URL] = true
			all = append(all, a)
		}
	}

	sort.Slice(all, func(i, j int) bool { return all[i].PubDate > all[j].PubDate })
	if len(all) > 60 {
		all = all[:60]
	}
	return all
}

// genericFeed decodes both RSS and Atom in a single pass.
type genericFeed struct {
	Items []struct {
		Title       string `xml:"title"`
		Link        string `xml:"link"`
		Description string `xml:"description"`
		Encoded     string `xml:"encoded"`
		PubDate     string `xml:"pubDate"`
		Enclosure   struct {
			URL  string `xml:"url,attr"`
			Type string `xml:"type,attr"`
		} `xml:"enclosure"`
		MediaContents []struct {
			URL    string `xml:"url,attr"`
			Medium string `xml:"medium,attr"`
			Type   string `xml:"type,attr"`
		} `xml:"http://search.yahoo.com/mrss/ content"`
		MediaThumbnail struct {
			URL string `xml:"url,attr"`
		} `xml:"http://search.yahoo.com/mrss/ thumbnail"`
	} `xml:"channel>item"`
	Entries []struct {
		Title string `xml:"title"`
		Links []struct {
			Href string `xml:"href,attr"`
			Rel  string `xml:"rel,attr"`
		} `xml:"link"`
		Summary        string `xml:"summary"`
		Content        string `xml:"content"`
		Updated        string `xml:"updated"`
		MediaContent   struct {
			URL string `xml:"url,attr"`
		} `xml:"http://search.yahoo.com/mrss/ content"`
		MediaThumbnail struct {
			URL string `xml:"url,attr"`
		} `xml:"http://search.yahoo.com/mrss/ thumbnail"`
	} `xml:"entry"`
}

func (f *Fetcher) fetchSource(src Source) ([]Article, error) {
	req, err := http.NewRequest(http.MethodGet, src.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "TheVanIsSecure/1.0 (Economic Dashboard; RSS Reader)")
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml, text/xml, */*")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var feed genericFeed
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, fmt.Errorf("XML decode: %w", err)
	}

	var articles []Article

	for _, item := range feed.Items {
		desc := item.Description
		if desc == "" {
			desc = item.Encoded
		}
		title   := stripHTML(item.Title)
		summary := truncate(stripHTML(desc), 240)
		if !matchesKeywords(title, summary, src.Keywords) {
			continue
		}
		t      := parseDate(item.PubDate)
		imgURL := feedImageFromItem(item.MediaContents, item.MediaThumbnail.URL, item.Enclosure.URL, item.Enclosure.Type, desc)
		articles = append(articles, Article{
			Title:    title,
			URL:      strings.TrimSpace(item.Link),
			Summary:  summary,
			Source:   src.Name,
			PubDate:  t.UTC().Format(time.RFC3339),
			PubLabel: humanDate(t),
			ImageURL: imgURL,
		})
	}

	for _, entry := range feed.Entries {
		link := atomLink(entry.Links)
		body := entry.Summary
		if body == "" {
			body = entry.Content
		}
		title   := stripHTML(entry.Title)
		summary := truncate(stripHTML(body), 240)
		if !matchesKeywords(title, summary, src.Keywords) {
			continue
		}
		t      := parseDate(entry.Updated)
		imgURL := entry.MediaContent.URL
		if imgURL == "" {
			imgURL = entry.MediaThumbnail.URL
		}
		if imgURL == "" {
			imgURL = extractImgSrc(body)
		}
		articles = append(articles, Article{
			Title:    title,
			URL:      link,
			Summary:  summary,
			Source:   src.Name,
			PubDate:  t.UTC().Format(time.RFC3339),
			PubLabel: humanDate(t),
			ImageURL: imgURL,
		})
	}

	return articles, nil
}

// feedImageFromItem picks the best image from RSS item fields.
func feedImageFromItem(mediaContents []struct {
	URL    string `xml:"url,attr"`
	Medium string `xml:"medium,attr"`
	Type   string `xml:"type,attr"`
}, thumbURL, encURL, encType, desc string) string {
	// Prefer media:content with image medium
	for _, mc := range mediaContents {
		if mc.URL != "" && (mc.Medium == "image" || strings.HasPrefix(mc.Type, "image/")) {
			return mc.URL
		}
	}
	// Any media:content URL
	for _, mc := range mediaContents {
		if isHTTP(mc.URL) {
			return mc.URL
		}
	}
	// media:thumbnail
	if isHTTP(thumbURL) {
		return thumbURL
	}
	// enclosure with image type
	if isHTTP(encURL) && strings.HasPrefix(encType, "image/") {
		return encURL
	}
	// First img src in description HTML
	return extractImgSrc(desc)
}

func extractImgSrc(html string) string {
	if m := imgSrcRe.FindStringSubmatch(html); len(m) > 1 {
		src := m[1]
		if isHTTP(src) && !isUIImage(src) {
			return src
		}
	}
	return ""
}

func atomLink(links []struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
}) string {
	for _, l := range links {
		if l.Rel == "alternate" && l.Href != "" {
			return l.Href
		}
	}
	for _, l := range links {
		if l.Href != "" {
			return l.Href
		}
	}
	return ""
}

// ── Date & text helpers ───────────────────────────────────────────────────────

var dateFormats = []string{
	time.RFC1123Z,
	time.RFC1123,
	time.RFC3339,
	"Mon, 2 Jan 2006 15:04:05 -0700",
	"Mon, 2 Jan 2006 15:04:05 MST",
	"2006-01-02T15:04:05Z",
	"2006-01-02T15:04:05-07:00",
	"2006-01-02",
}

func parseDate(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, f := range dateFormats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func humanDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	diff := time.Since(t)
	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		return fmt.Sprintf("%dm ago", int(diff.Minutes()))
	case diff < 24*time.Hour:
		h := int(diff.Hours())
		if h == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", h)
	case diff < 48*time.Hour:
		return "yesterday"
	default:
		return t.Format("Jan 2")
	}
}

func stripHTML(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	result := b.String()
	for _, e := range [][2]string{
		{"&amp;", "&"}, {"&lt;", "<"}, {"&gt;", ">"}, {"&quot;", `"`},
		{"&#39;", "'"}, {"&nbsp;", " "}, {"&#8217;", "'"}, {"&#8216;", "'"},
		{"&#8220;", `"`}, {"&#8221;", `"`}, {"&#8230;", "..."}, {"&#8212;", "—"},
		{"&#8211;", "–"}, {"&apos;", "'"},
	} {
		result = strings.ReplaceAll(result, e[0], e[1])
	}
	return strings.TrimSpace(strings.Join(strings.Fields(result), " "))
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if idx := strings.LastIndex(s[:max], " "); idx > max/2 {
		return s[:idx] + "…"
	}
	return s[:max] + "…"
}
