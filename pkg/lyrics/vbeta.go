package lyrics

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/betterlyrics/bete-node/pkg/cache"
	"github.com/betterlyrics/bete-node/pkg/upstream"
)

// RawResponse holds the pure upstream byte response and content-type
type RawResponse struct {
	Body        []byte
	ContentType string
	StatusCode  int
	Cached      bool
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

// FetchRawLyrics queries available providers and returns the exact raw payload from upstream
func (s *Service) FetchRawLyrics(title, author, provider string) (*RawResponse, error) {
	title = cleanString(title)
	author = cleanString(author)
	provider = strings.ToLower(strings.TrimSpace(provider))

	cacheKey := fmt.Sprintf("vbeta_raw:%s|%s|%s", title, author, provider)

	// Check cache for instant replay (< 1ms)
	if item, found := s.cache.Get(cacheKey); found {
		ct := item.Header.Get("Content-Type")
		if ct == "" {
			ct = "application/json; charset=utf-8"
		}
		return &RawResponse{
			Body:        item.Value,
			ContentType: ct,
			StatusCode:  http.StatusOK,
			Cached:      true,
		}, nil
	}

	var res *RawResponse
	var err error

	switch provider {
	case "unison":
		res, err = s.fetchRawUnison(title, author)
	case "lrclib":
		res, err = s.fetchRawLRCLIB(title, author)
	default: // "auto"
		// Default to LRCLib raw, fallback to Unison raw
		res, err = s.fetchRawLRCLIB(title, author)
		if err != nil || (res != nil && res.StatusCode != http.StatusOK) {
			if uRes, uErr := s.fetchRawUnison(title, author); uErr == nil && uRes.StatusCode == http.StatusOK {
				res = uRes
				err = nil
			}
		}
	}

	if err != nil {
		return nil, err
	}

	if res == nil {
		return &RawResponse{
			Body:        []byte(`{"error":true,"message":"Lyrics not found"}`),
			ContentType: "application/json; charset=utf-8",
			StatusCode:  http.StatusNotFound,
		}, nil
	}

	// Cache successful upstream response
	if res.StatusCode == http.StatusOK && len(res.Body) > 0 {
		header := make(http.Header)
		header.Set("Content-Type", res.ContentType)
		s.cache.Set(cacheKey, res.Body, header, 48*time.Hour)
	}

	return res, nil
}

// fetchRawLRCLIB retrieves pure 1:1 JSON from lrclib.net
func (s *Service) fetchRawLRCLIB(title, author string) (*RawResponse, error) {
	apiURL := fmt.Sprintf("https://lrclib.net/api/get?track_name=%s&artist_name=%s",
		url.QueryEscape(title),
		url.QueryEscape(author),
	)

	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "BetterLyrics/1.0.0 (https://github.com/dako-disturb0/betenode)")

	resp, err := s.client.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// Fallback to lrclib search query
		return s.searchRawLRCLIB(title, author)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/json; charset=utf-8"
	}

	return &RawResponse{
		Body:        body,
		ContentType: ct,
		StatusCode:  resp.StatusCode,
		Cached:      false,
	}, nil
}

func (s *Service) searchRawLRCLIB(title, author string) (*RawResponse, error) {
	q := strings.TrimSpace(title + " " + author)
	apiURL := fmt.Sprintf("https://lrclib.net/api/search?q=%s", url.QueryEscape(q))

	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "BetterLyrics/1.0.0")

	resp, err := s.client.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/json; charset=utf-8"
	}

	return &RawResponse{
		Body:        body,
		ContentType: ct,
		StatusCode:  resp.StatusCode,
		Cached:      false,
	}, nil
}

// fetchRawUnison retrieves pure 1:1 JSON from Unison
func (s *Service) fetchRawUnison(title, author string) (*RawResponse, error) {
	apiURL := fmt.Sprintf("https://unison.boidu.dev/lyrics?title=%s&artist=%s",
		url.QueryEscape(title),
		url.QueryEscape(author),
	)

	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "BetterLyrics/1.0.0")

	resp, err := s.client.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/json; charset=utf-8"
	}

	return &RawResponse{
		Body:        body,
		ContentType: ct,
		StatusCode:  resp.StatusCode,
		Cached:      false,
	}, nil
}

func cleanString(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\"")
	s = strings.Trim(s, "'")
	return s
}
