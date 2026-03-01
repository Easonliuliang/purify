package engine

import (
	"sync"
	"time"
)

// domainEntry stores the preferred engine for a domain with a TTL.
type domainEntry struct {
	engineName string
	expiresAt  time.Time
}

// DomainMemory remembers which engine worked best for each domain.
// Entries expire after the configured TTL and are cleaned up periodically.
type DomainMemory struct {
	store sync.Map // domain (string) -> *domainEntry
	ttl   time.Duration
	done  chan struct{}
}

// NewDomainMemory creates a DomainMemory with the given TTL and starts
// a background goroutine that prunes expired entries every hour.
func NewDomainMemory(ttl time.Duration) *DomainMemory {
	dm := &DomainMemory{
		ttl:  ttl,
		done: make(chan struct{}),
	}
	go dm.cleanupLoop()
	return dm
}

// Get returns the remembered engine name for a domain, or "" if not found / expired.
func (dm *DomainMemory) Get(domain string) string {
	val, ok := dm.store.Load(domain)
	if !ok {
		return ""
	}
	entry := val.(*domainEntry)
	if time.Now().After(entry.expiresAt) {
		dm.store.Delete(domain)
		return ""
	}
	return entry.engineName
}

// Set records which engine succeeded for a domain.
func (dm *DomainMemory) Set(domain, engineName string) {
	dm.store.Store(domain, &domainEntry{
		engineName: engineName,
		expiresAt:  time.Now().Add(dm.ttl),
	})
}

// Delete removes the memory for a domain (e.g. after the remembered engine fails).
func (dm *DomainMemory) Delete(domain string) {
	dm.store.Delete(domain)
}

// Stop terminates the background cleanup goroutine.
func (dm *DomainMemory) Stop() {
	close(dm.done)
}

// cleanupLoop runs every hour, deleting expired entries.
func (dm *DomainMemory) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-dm.done:
			return
		case <-ticker.C:
			now := time.Now()
			dm.store.Range(func(key, value any) bool {
				entry := value.(*domainEntry)
				if now.After(entry.expiresAt) {
					dm.store.Delete(key)
				}
				return true
			})
		}
	}
}
