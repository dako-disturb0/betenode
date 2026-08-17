package origin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/betterlyrics/bete-node/pkg/cache"
	"github.com/betterlyrics/bete-node/pkg/config"
	"github.com/betterlyrics/bete-node/pkg/lyrics"
	"github.com/betterlyrics/bete-node/pkg/upstream"
)

// Handler manages Origin HTTP requests, load balancing, caching, and fallback
type Handler struct {
	cfg            *config.Config
	cache          *cache.MemoryCache
	pool           *NodePool
	upstreamClient *upstream.Client
	nodeClient     *upstream.Client
	lyricsService  *lyrics.Service
	startTime      time.Time
}

// NewHandler creates a new Origin handler
func NewHandler(cfg *config.Config, memCache *cache.MemoryCache, pool *NodePool) *Handler {
	return &Handler{
		cfg:            cfg,
		cache:          memCache,
		pool:           pool,
		upstreamClient: upstream.NewClient(cfg.UpstreamURL, 30*time.Second),
		nodeClient:     upstream.NewClient("", 25*time.Second),
		lyricsService:  lyrics.NewService(memCache),
		startTime:      time.Now(),
	}
}

// NodesHandler returns JSON dashboard status of all configured nodes
func (h *Handler) NodesHandler(w http.ResponseWriter, r *http.Request) {
	enableCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	statuses := h.pool.GetAllStatus()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"origin": map[string]interface{}{
			"role":        "origin_orchestrator",
			"platform":    h.cfg.Platform.String(),
			"upstream":    h.cfg.UpstreamURL,
			"uptime_sec":  int64(time.Since(h.startTime).Seconds()),
			"cache_stats": h.cache.Stats(),
		},
		"total_nodes": len(statuses),
		"nodes":       statuses,
	})
}

// LyricsHandler handles POST /v2/lyrics with smart routing to fastest node or fallback to upstream
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

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Parse form params for master cache
	parsedValues, _ := url.ParseQuery(string(bodyBytes))
	videoId := parsedValues.Get("videoId")
	song := parsedValues.Get("song")
	artist := parsedValues.Get("artist")
	duration := parsedValues.Get("duration")
	isrc := parsedValues.Get("isrc")

	cacheKey := cache.BuildLyricsKey(videoId, song, artist, duration, isrc)

	// 1. Check Master Cache first (< 1ms instant replay)
	if item, found := h.cache.Get(cacheKey); found {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-BetterLyrics-Cache", "HIT-ORIGIN")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(item.Value)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return
	}

	// 2. Select best active node from pool
	targetURL := ""
	routedNode := h.pool.GetBestNode()
	if routedNode != nil {
		if strings.HasSuffix(routedNode.Endpoint.BaseURL, "/exec") {
			targetURL = fmt.Sprintf("%s?path=v2/lyrics", routedNode.Endpoint.BaseURL)
		} else {
			targetURL = fmt.Sprintf("%s/v2/lyrics", strings.TrimRight(routedNode.Endpoint.BaseURL, "/"))
		}
	} else {
		// No healthy nodes, fallback directly to official upstream
		targetURL = fmt.Sprintf("%s/v2/lyrics", strings.TrimRight(h.cfg.UpstreamURL, "/"))
	}

	// Try proxying to chosen target (with failover)
	success := h.proxyLyricsStream(w, r, targetURL, bodyBytes, cacheKey, routedNode != nil)
	if !success && routedNode != nil {
		// Failover to upstream
		log.Printf("[Origin] ⚠️ Node %s failed, failing over to upstream %s", routedNode.Endpoint.BaseURL, h.cfg.UpstreamURL)
		h.pool.markFailed(routedNode)
		fallbackURL := fmt.Sprintf("%s/v2/lyrics", strings.TrimRight(h.cfg.UpstreamURL, "/"))
		_ = h.proxyLyricsStream(w, r, fallbackURL, bodyBytes, cacheKey, false)
	}
}

func (h *Handler) proxyLyricsStream(w http.ResponseWriter, r *http.Request, targetURL string, bodyBytes []byte, cacheKey string, isNode bool) bool {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return false
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if ua := r.Header.Get("User-Agent"); ua != "" {
		req.Header.Set("User-Agent", ua)
	}

	client := h.nodeClient.HTTPClient
	if !isNode {
		client = h.upstreamClient.HTTPClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return false
	}

	// Forward headers
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("X-BetterLyrics-Cache", "MISS-ORIGIN")
	if isNode {
		w.Header().Set("X-BetterLyrics-Routed", "EdgeNode")
	} else {
		w.Header().Set("X-BetterLyrics-Routed", "DirectUpstream")
	}
	w.WriteHeader(resp.StatusCode)

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

	if resp.StatusCode == http.StatusOK && buffer.Len() > 0 && strings.Contains(buffer.String(), "event:") {
		h.cache.Set(cacheKey, buffer.Bytes(), resp.Header, time.Duration(h.cfg.CacheTTLSec)*time.Second)
	}

	return true
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

	resp, err := h.upstreamClient.HTTPClient.Do(req)
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

// VbetaLyricsHandler handles direct GET /vbeta/lyrics/ queries
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
	// If provider is specified in path like /vbeta/lyrics/lrclib or /vbeta/lyrics/unison
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
		w.Header().Set("X-BetterLyrics-Cache", "HIT-VBETA-RAW")
	} else {
		w.Header().Set("X-BetterLyrics-Cache", "MISS-VBETA-RAW")
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

// Server wraps the HTTP listener for Origin
type Server struct {
	httpServer *http.Server
	cfg        *config.Config
	handler    *Handler
	pool       *NodePool
}

// NewServer creates a new Origin server instance
func NewServer(cfg *config.Config, memCache *cache.MemoryCache) *Server {
	pool := NewNodePool(cfg.Nodes, cfg.AutoHealth, cfg.HealthSec)
	h := NewHandler(cfg, memCache, pool)
	mux := http.NewServeMux()

	mux.HandleFunc("/v2/lyrics", h.LyricsHandler)
	mux.HandleFunc("/vbeta/lyrics", h.VbetaLyricsHandler)
	mux.HandleFunc("/vbeta/lyrics/", h.VbetaLyricsHandler)
	mux.HandleFunc("/verify-turnstile", h.TurnstileProxyHandler)
	mux.HandleFunc("/nodes", h.NodesHandler)
	mux.HandleFunc("/status", h.NodesHandler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		enableCORS(w)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("OK"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			enableCORS(w)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"service":     "BetterLyrics Origin Orchestrator",
				"status":      "running",
				"platform":    cfg.Platform.String(),
				"total_nodes": len(cfg.Nodes),
				"endpoints": []string{
					"/v2/lyrics",
					"/verify-turnstile",
					"/nodes",
					"/status",
					"/health",
				},
			})
			return
		}
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
		pool:    pool,
	}
}

// Start runs the Origin server
func (s *Server) Start() error {
	log.Printf("[Origin] 🌟 BetterLyrics Origin Orchestrator running on %s (Platform: %s)", s.httpServer.Addr, s.cfg.Platform.String())
	log.Printf("[Origin] 🔗 Configured Nodes (%d):", len(s.cfg.Nodes))
	for _, n := range s.cfg.Nodes {
		log.Printf("  -> [NODE%d] %s (Interconnect: %s)", n.ID, n.BaseURL, n.InterPath)
	}
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully stops the server
func (s *Server) Shutdown(ctx context.Context) error {
	s.pool.Close()
	return s.httpServer.Shutdown(ctx)
}

// ServeHTTP implements http.Handler for serverless environments (Vercel / Netlify)
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.httpServer.Handler.ServeHTTP(w, r)
}
