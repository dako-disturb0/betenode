package lyrics

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/betterlyrics/bete-node/pkg/cache"
	"github.com/betterlyrics/bete-node/pkg/upstream"
)

// LyricsResponse is the structured JSON output of /vbeta/lyrics
type LyricsResponse struct {
	Status       string                 `json:"status"`
	Source       string                 `json:"source"`
	Title        string                 `json:"title"`
	Author       string                 `json:"author"`
	Album        string                 `json:"album,omitempty"`
	Duration     int                    `json:"duration_seconds,omitempty"`
	SyncedLyrics string                 `json:"synced_lyrics,omitempty"`
	PlainLyrics  string                 `json:"plain_lyrics,omitempty"`
	Cached       bool                   `json:"cached"`
	Raw          map[string]interface{} `json:"raw,omitempty"`
}

// Service manages multi-provider direct lyrics retrieval
type Service struct {
	client *upstream.Client
	cache  *cache.MemoryCache
}

// NewService creates a new Lyrics Service
func NewService(memCache *cache.MemoryCache) *Service {
	return &Service{
		client: upstream.NewClient("", 10*time.Second),
		cache:  memCache,
	}
}

// FetchLyrics queries available providers for the requested song
func (s *Service) FetchLyrics(title, author, provider string) (*LyricsResponse, error) {
	title = cleanString(title)
	author = cleanString(author)
	provider = strings.ToLower(strings.TrimSpace(provider))

	cacheKey := fmt.Sprintf("vbeta:%s|%s|%s", title, author, provider)

	// Check cache
	if item, found := s.cache.Get(cacheKey); found {
		var cachedResp LyricsResponse
		if err := json.Unmarshal(item.Value, &cachedResp); err == nil {
			cachedResp.Cached = true
			return &cachedResp, nil
		}
	}

	var res *LyricsResponse
	var err error

	// Route based on requested provider
	switch provider {
	case "unison":
		res, err = s.fetchFromUnison(title, author)
	case "lrclib":
		res, err = s.fetchFromLRCLIB(title, author)
	default: // "auto" or empty
		// Try LRCLib first (fastest open database)
		res, err = s.fetchFromLRCLIB(title, author)
		if err != nil || (res != nil && res.SyncedLyrics == "" && res.PlainLyrics == "") {
			// Fallback to Unison
			if uRes, uErr := s.fetchFromUnison(title, author); uErr == nil && (uRes.SyncedLyrics != "" || uRes.PlainLyrics != "") {
				res = uRes
				err = nil
			}
		}
	}

	if err != nil {
		return nil, err
	}

	if res == nil || (res.SyncedLyrics == "" && res.PlainLyrics == "") {
		return &LyricsResponse{
			Status: "not_found",
			Source: provider,
			Title:  title,
			Author: author,
		}, nil
	}

	res.Status = "success"
	res.Cached = false

	// Save to cache
	if bytesVal, mErr := json.Marshal(res); mErr == nil {
		s.cache.Set(cacheKey, bytesVal, nil, 24*time.Hour)
	}

	return res, nil
}

// fetchFromLRCLIB fetches lyrics from lrclib.net
func (s *Service) fetchFromLRCLIB(title, author string) (*LyricsResponse, error) {
	apiURL := fmt.Sprintf("https://lrclib.net/api/get?track_name=%s&artist_name=%s",
		url.QueryEscape(title),
		url.QueryEscape(author),
	)

	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "BetterLyrics-Accelerator/1.0.0 (https://github.com/dako-disturb0/betenode)")

	resp, err := s.client.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// Try search endpoint if exact match fails
		return s.searchLRCLIB(title, author)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("LRCLib returned status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var data struct {
		TrackName    string  `json:"trackName"`
		ArtistName   string  `json:"artistName"`
		AlbumName    string  `json:"albumName"`
		Duration     float64 `json:"duration"`
		SyncedLyrics string  `json:"syncedLyrics"`
		PlainLyrics  string  `json:"plainLyrics"`
	}

	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	return &LyricsResponse{
		Source:       "LRCLIB",
		Title:        data.TrackName,
		Author:       data.ArtistName,
		Album:        data.AlbumName,
		Duration:     int(data.Duration),
		SyncedLyrics: data.SyncedLyrics,
		PlainLyrics:  data.PlainLyrics,
	}, nil
}

// searchLRCLIB fallback search endpoint
func (s *Service) searchLRCLIB(title, author string) (*LyricsResponse, error) {
	q := strings.TrimSpace(title + " " + author)
	apiURL := fmt.Sprintf("https://lrclib.net/api/search?q=%s", url.QueryEscape(q))

	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "BetterLyrics-Accelerator/1.0.0 (https://github.com/dako-disturb0/betenode)")

	resp, err := s.client.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("LRCLib search status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var list []struct {
		TrackName    string  `json:"trackName"`
		ArtistName   string  `json:"artistName"`
		AlbumName    string  `json:"albumName"`
		Duration     float64 `json:"duration"`
		SyncedLyrics string  `json:"syncedLyrics"`
		PlainLyrics  string  `json:"plainLyrics"`
	}

	if err := json.Unmarshal(body, &list); err != nil || len(list) == 0 {
		return nil, nil
	}

	first := list[0]
	return &LyricsResponse{
		Source:       "LRCLIB",
		Title:        first.TrackName,
		Author:       first.ArtistName,
		Album:        first.AlbumName,
		Duration:     int(first.Duration),
		SyncedLyrics: first.SyncedLyrics,
		PlainLyrics:  first.PlainLyrics,
	}, nil
}

// fetchFromUnison fetches lyrics from Unison API
func (s *Service) fetchFromUnison(title, author string) (*LyricsResponse, error) {
	apiURL := fmt.Sprintf("https://unison.boidu.dev/lyrics?title=%s&artist=%s",
		url.QueryEscape(title),
		url.QueryEscape(author),
	)

	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "BetterLyrics-Accelerator/1.0.0")

	resp, err := s.client.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Unison API status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var data map[string]interface{}
	_ = json.Unmarshal(body, &data)

	synced := ""
	plain := ""

	if s, ok := data["lyrics"].(string); ok {
		if strings.Contains(s, "[") && strings.Contains(s, "]") {
			synced = s
		} else {
			plain = s
		}
	}

	return &LyricsResponse{
		Source:       "Unison",
		Title:        title,
		Author:       author,
		SyncedLyrics: synced,
		PlainLyrics:  plain,
		Raw:          data,
	}, nil
}

func cleanString(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\"")
	s = strings.Trim(s, "'")
	return s
}
