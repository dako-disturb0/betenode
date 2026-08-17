package cache

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Item holds cached data and metadata
type Item struct {
	Key       string
	Value     []byte
	Header    http.Header
	ExpiresAt time.Time
	CreatedAt time.Time
	Hits      uint64
}

// Stats holds cache metrics
type Stats struct {
	TotalItems int    `json:"total_items"`
	MaxItems   int    `json:"max_items"`
	Hits       uint64 `json:"hits"`
	Misses     uint64 `json:"misses"`
	HitRate    string `json:"hit_rate"`
}

// MemoryCache is a concurrent LRU cache with TTL eviction
type MemoryCache struct {
	mu         sync.RWMutex
	items      map[string]*list.Element
	evictList  *list.List
	maxItems   int
	defaultTTL time.Duration
	hits       uint64
	misses     uint64
}

// NewMemoryCache creates a new MemoryCache instance
func NewMemoryCache(maxItems int, defaultTTL time.Duration) *MemoryCache {
	if maxItems <= 0 {
		maxItems = 10000
	}
	if defaultTTL <= 0 {
		defaultTTL = 24 * time.Hour
	}

	c := &MemoryCache{
		items:      make(map[string]*list.Element),
		evictList:  list.New(),
		maxItems:   maxItems,
		defaultTTL: defaultTTL,
	}

	// Auto garbage collection for expired items
	go c.startGC(2 * time.Minute)
	return c
}

// Get retrieves an item by key
func (c *MemoryCache) Get(key string) (*Item, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, exists := c.items[key]
	if !exists {
		c.misses++
		return nil, false
	}

	item := elem.Value.(*Item)
	if time.Now().After(item.ExpiresAt) {
		c.removeElement(elem)
		c.misses++
		return nil, false
	}

	c.evictList.MoveToFront(elem)
	item.Hits++
	c.hits++
	return item, true
}

// Set adds or replaces an item in cache
func (c *MemoryCache) Set(key string, val []byte, header http.Header, ttl time.Duration) {
	if ttl <= 0 {
		ttl = c.defaultTTL
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	exp := now.Add(ttl)

	// Clone header
	hCopy := make(http.Header)
	if header != nil {
		for k, vv := range header {
			hCopy[k] = append([]string(nil), vv...)
		}
	}

	if elem, exists := c.items[key]; exists {
		c.evictList.MoveToFront(elem)
		item := elem.Value.(*Item)
		item.Value = val
		item.Header = hCopy
		item.ExpiresAt = exp
		return
	}

	// Evict oldest if full
	for c.evictList.Len() >= c.maxItems {
		c.removeOldest()
	}

	item := &Item{
		Key:       key,
		Value:     val,
		Header:    hCopy,
		ExpiresAt: exp,
		CreatedAt: now,
	}
	elem := c.evictList.PushFront(item)
	c.items[key] = elem
}

func (c *MemoryCache) removeElement(elem *list.Element) {
	c.evictList.Remove(elem)
	item := elem.Value.(*Item)
	delete(c.items, item.Key)
}

func (c *MemoryCache) removeOldest() {
	elem := c.evictList.Back()
	if elem != nil {
		c.removeElement(elem)
	}
}

func (c *MemoryCache) startGC(interval time.Duration) {
	ticker := time.NewTicker(interval)
	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for _, elem := range c.items {
			item := elem.Value.(*Item)
			if now.After(item.ExpiresAt) {
				c.removeElement(elem)
			}
		}
		c.mu.Unlock()
	}
}

// Stats returns current cache statistics
func (c *MemoryCache) Stats() Stats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	totalReq := c.hits + c.misses
	hitRate := "0%"
	if totalReq > 0 {
		rate := (float64(c.hits) / float64(totalReq)) * 100.0
		hitRate = fmt.Sprintf("%.2f%%", rate)
	}

	return Stats{
		TotalItems: len(c.items),
		MaxItems:   c.maxItems,
		Hits:       c.hits,
		Misses:     c.misses,
		HitRate:    hitRate,
	}
}

// BuildLyricsKey builds a deterministic SHA-256 cache key from request parameters
func BuildLyricsKey(videoId, song, artist, duration, isrc string) string {
	raw := strings.ToLower(fmt.Sprintf("%s|%s|%s|%s|%s",
		strings.TrimSpace(videoId),
		strings.TrimSpace(song),
		strings.TrimSpace(artist),
		strings.TrimSpace(duration),
		strings.TrimSpace(isrc),
	))
	hash := sha256.Sum256([]byte(raw))
	return "blyrics:" + hex.EncodeToString(hash[:16])
}
