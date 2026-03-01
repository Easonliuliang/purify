package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"github.com/use-agent/purify/models"
)

// entry holds a cached response with its creation timestamp.
type entry struct {
	response  *models.ScrapeResponse
	createdAt time.Time
}

// Cache is a simple in-memory cache for scrape responses.
// It is safe for concurrent use.
type Cache struct {
	mu         sync.RWMutex
	store      map[string]*entry
	maxEntries int
}

// New creates a new Cache with the given maximum number of entries.
// A background goroutine runs every 5 minutes to evict expired entries
// (older than 1 hour).
func New(maxEntries int) *Cache {
	c := &Cache{
		store:      make(map[string]*entry),
		maxEntries: maxEntries,
	}

	go c.cleanupLoop()
	return c
}

// Key generates a cache key from the URL, output format, and extract mode.
func Key(url, outputFormat, extractMode string) string {
	h := sha256.New()
	h.Write([]byte(url))
	h.Write([]byte("|"))
	h.Write([]byte(outputFormat))
	h.Write([]byte("|"))
	h.Write([]byte(extractMode))
	return hex.EncodeToString(h.Sum(nil))
}

// Get retrieves a cached response if it exists and is younger than maxAge.
// maxAge is in milliseconds. If maxAge <= 0, no cache lookup is performed.
// Returns the response and whether it was a cache hit.
func (c *Cache) Get(key string, maxAgeMs int) (*models.ScrapeResponse, bool) {
	if maxAgeMs <= 0 {
		return nil, false
	}

	c.mu.RLock()
	e, ok := c.store[key]
	c.mu.RUnlock()

	if !ok {
		return nil, false
	}

	maxAge := time.Duration(maxAgeMs) * time.Millisecond
	if time.Since(e.createdAt) > maxAge {
		return nil, false
	}

	return e.response, true
}

// Set stores a response in the cache. If the cache is at capacity,
// a random entry is evicted to make room.
func (c *Cache) Set(key string, resp *models.ScrapeResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict one random entry if at capacity (map iteration is random in Go).
	if len(c.store) >= c.maxEntries {
		for k := range c.store {
			delete(c.store, k)
			break
		}
	}

	c.store[key] = &entry{
		response:  resp,
		createdAt: time.Now(),
	}
}

// cleanupLoop evicts entries older than 1 hour every 5 minutes.
func (c *Cache) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Add(-1 * time.Hour)
		c.mu.Lock()
		for k, e := range c.store {
			if e.createdAt.Before(cutoff) {
				delete(c.store, k)
			}
		}
		c.mu.Unlock()
	}
}
