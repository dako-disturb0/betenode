package node

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/betterlyrics/bete-node/pkg/cache"
	"github.com/betterlyrics/bete-node/pkg/config"
	"github.com/betterlyrics/bete-node/pkg/lyrics"
	"github.com/betterlyrics/bete-node/pkg/upstream"
)

// InterconnectStatus represents the payload returned by /interconnect
type InterconnectStatus struct {
	Status     string      `json:"status"`
	Role       string      `json:"role"`
	Version    string      `json:"version"`
	Platform   string      `json:"platform"`
	UptimeSec  int64       `json:"uptime_sec"`
	Goroutines int         `json:"goroutines"`
	MemoryMB   float64     `json:"memory_mb"`
	Cache      cache.Stats `json:"cache"`
	Timestamp  int64       `json:"timestamp"`
	ServerTime string      `json:"server_time"`
}

// Handler contains logic for edge node proxying
type Handler struct {
	cfg           *config.Config
	cache         *cache.MemoryCache
	client        *upstream.Client
	lyricsService *lyrics.Service
	startTime     time.Time
}

// NewHandler initializes a Node handler
func NewHandler(cfg *config.Config, memCache *cache.MemoryCache) *Handler {
	return &Handler{
		cfg:           cfg,
		cache:         memCache,
		client:        upstream.NewClient(cfg.UpstreamURL, 30*time.Second),
		lyricsService: lyrics.NewService(memCache),
		startTime:     time.Now(),
	}
}

// InterconnectHandler handles /interconnect telemetries
func (h *Handler) InterconnectHandler(w http.ResponseWriter, r *http.Request) {
	enableCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	status := InterconnectStatus{
		Status:     "online",
		Role:       "edge_node",
		Version:    "1.0.0",
		Platform:   h.cfg.Platform.String(),
		UptimeSec:  int64(time.Since(h.startTime).Seconds()),
		Goroutines: runtime.NumGoroutine(),
		MemoryMB:   float64(m.Alloc) / 1024 / 1024,
		Cache:      h.cache.Stats(),
		Timestamp:  time.Now().Unix(),
		ServerTime: time.Now().UTC().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(status)
}

// HealthHandler returns quick 200 OK
func (h *Handler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	enableCORS(w)
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

// LyricsHandler handles POST /v2/lyrics with SSE streaming & caching
func (h *Handler) LyricsHandler(w http.ResponseWriter, r *http.Request) {
	enableCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read body for cache key & upstream request
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Parse form params for key generation
	parsedValues, _ := url.ParseQuery(string(bodyBytes))
	videoId := parsedValues.Get("videoId")
	song := parsedValues.Get("song")
	artist := parsedValues.Get("artist")
	duration := parsedValues.Get("duration")
	isrc := parsedValues.Get("isrc")

	cacheKey := cache.BuildLyricsKey(videoId, song, artist, duration, isrc)

	// Check in-memory cache
	if item, found := h.cache.Get(cacheKey); found {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-BetterLyrics-Cache", "HIT-NODE")
		w.Header().Set("X-BetterLyrics-Node", "Edge")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(item.Value)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return
	}

	// Stream from upstream
	upstreamURL := fmt.Sprintf("%s/v2/lyrics", strings.TrimRight(h.cfg.UpstreamURL, "/"))
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, bytes.NewReader(bodyBytes))
	if err != nil {
		http.Error(w, "Failed to create upstream request", http.StatusInternalServerError)
		return
	}

	// Copy headers from original request
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if ua := r.Header.Get("User-Agent"); ua != "" {
		req.Header.Set("User-Agent", ua)
	}

	resp, err := h.client.HTTPClient.Do(req)
	if err != nil {
		log.Printf("[Node] Upstream error: %v", err)
		http.Error(w, "Upstream connection failure", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Forward headers
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("X-BetterLyrics-Cache", "MISS-NODE")
	w.Header().Set("X-BetterLyrics-Node", "Edge")
	w.WriteHeader(resp.StatusCode)

	// TeeReader to capture SSE buffer for caching
	var buffer bytes.Buffer
	flusher, isFlusher := w.(http.Flusher)

	chunk := make([]byte, 1024)
	for {
		n, readErr := resp.Body.Read(chunk)
		if n > 0 {
			buffer.Write(chunk[:n])
			_, _ = w.Write(chunk[:n])
			if isFlusher {
				flusher.Flush()
			}
		}
		if readErr != nil {
			break
		}
	}

	// Cache successful SSE result if valid
	if resp.StatusCode == http.StatusOK && buffer.Len() > 0 && strings.Contains(buffer.String(), "event:") {
		h.cache.Set(cacheKey, buffer.Bytes(), resp.Header, time.Duration(h.cfg.CacheTTLSec)*time.Second)
	}
}

// TurnstileProxyHandler proxies /verify-turnstile to upstream
func (h *Handler) TurnstileProxyHandler(w http.ResponseWriter, r *http.Request) {
	enableCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	bodyBytes, _ := io.ReadAll(r.Body)
	defer r.Body.Close()

	upstreamURL := fmt.Sprintf("%s/verify-turnstile", strings.TrimRight(h.cfg.UpstreamURL, "/"))
	req, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, bytes.NewReader(bodyBytes))
	if err != nil {
		http.Error(w, "Failed to create upstream request", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.client.HTTPClient.Do(req)
	if err != nil {
		http.Error(w, "Upstream Turnstile verification failure", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// VbetaLyricsHandler handles direct GET /vbeta/lyrics/ queries on Node
func (h *Handler) VbetaLyricsHandler(w http.ResponseWriter, r *http.Request) {
	enableCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	q := r.URL.Query()
	title := q.Get("title")
	if title == "" {
		title = q.Get("song")
	}
	if title == "" {
		title = q.Get("track")
	}

	author := q.Get("author")
	if author == "" {
		author = q.Get("artist")
	}

	provider := q.Get("provider")
	trimmedPath := strings.Trim(strings.TrimPrefix(r.URL.Path, "/vbeta/lyrics"), "/")
	if trimmedPath != "" && provider == "" {
		provider = trimmedPath
	}

	if title == "" && author == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error":   true,
			"message": "Missing required query parameters. Usage: /vbeta/lyrics/?title=\"night dancer\"&author=\"imase\"/(provider)",
			"example": "/vbeta/lyrics/?title=night%20dancer&author=imase",
		})
		return
	}

	res, err := h.lyricsService.FetchRawLyrics(title, author, provider)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error":   true,
			"message": err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", res.ContentType)
	if res.Cached {
		w.Header().Set("X-BetterLyrics-Cache", "HIT-VBETA-RAW-NODE")
	} else {
		w.Header().Set("X-BetterLyrics-Cache", "MISS-VBETA-RAW-NODE")
	}
	w.WriteHeader(res.StatusCode)
	_, _ = w.Write(res.Body)
}

func enableCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, HEAD")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Access-Control-Expose-Headers", "*")
}

// Server wraps the HTTP listener for Node
type Server struct {
	httpServer *http.Server
	cfg        *config.Config
	handler    *Handler
}

// NewServer creates a new Node server instance
func NewServer(cfg *config.Config, memCache *cache.MemoryCache) *Server {
	h := NewHandler(cfg, memCache)
	mux := http.NewServeMux()

	mux.HandleFunc("/interconnect", h.InterconnectHandler)
	mux.HandleFunc("/v2/lyrics", h.LyricsHandler)
	mux.HandleFunc("/vbeta/lyrics", h.VbetaLyricsHandler)
	mux.HandleFunc("/vbeta/lyrics/", h.VbetaLyricsHandler)
	mux.HandleFunc("/verify-turnstile", h.TurnstileProxyHandler)
	mux.HandleFunc("/health", h.HealthHandler)
	mux.HandleFunc("/ping", h.HealthHandler)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			enableCORS(w)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"service":  "BetterLyrics Edge Accelerator Node",
				"status":   "running",
				"platform": cfg.Platform.String(),
				"docs":     "/interconnect",
			})
			return
		}
		// Fallback proxy to upstream for other paths
		http.NotFound(w, r)
	})

	addr := fmt.Sprintf("%s:%s", cfg.Host, cfg.Port)
	return &Server{
		httpServer: &http.Server{
			Addr:         addr,
			Handler:      mux,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 60 * time.Second,
			IdleTimeout:  120 * time.Second,
		},
		cfg:     cfg,
		handler: h,
	}
}

// Start runs the HTTP server
func (s *Server) Start() error {
	log.Printf("[Node] 🚀 BetterLyrics Edge Node running on %s (Platform: %s)", s.httpServer.Addr, s.cfg.Platform.String())
	log.Printf("[Node] 📡 Upstream Origin: %s", s.cfg.UpstreamURL)
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully stops the server
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// ServeHTTP implements http.Handler for serverless environments (Vercel / Netlify)
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.httpServer.Handler.ServeHTTP(w, r)
}
