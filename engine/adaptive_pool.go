package engine

import (
	"log/slog"
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// PageHandle wraps a generic pool entry with health tracking metadata.
type PageHandle struct {
	ID       int64
	errScore float64
	useCount int
	created  time.Time
	mu       sync.Mutex
}

// NewPageHandle creates a new PageHandle with the given ID.
func NewPageHandle(id int64) *PageHandle {
	return &PageHandle{
		ID:      id,
		created: time.Now(),
	}
}

// RecordSuccess decreases the error score (min 0).
func (h *PageHandle) RecordSuccess() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.useCount++
	h.errScore = math.Max(0, h.errScore-0.5)
}

// RecordFailure increases the error score.
func (h *PageHandle) RecordFailure() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.useCount++
	h.errScore += 1.0
}

// ShouldRetire returns true if the page should be retired based on health metrics.
func (h *PageHandle) ShouldRetire() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.errScore >= 3.0 {
		return true
	}
	if h.useCount >= 50 {
		return true
	}
	if time.Since(h.created) >= 50*time.Minute {
		return true
	}
	return false
}

// AdaptivePoolConfig holds configuration for the adaptive pool.
type AdaptivePoolConfig struct {
	MinPages     int
	HardMax      int
	MemThreshold float64 // 0.0–1.0, fraction of system memory
	ScaleStep    float64 // 0.0–1.0, fraction to grow/shrink
}

// PageFactory creates a new page and returns its handle ID.
// The caller is responsible for managing the underlying resource.
type PageFactory func() (int64, error)

// PageDestroyer closes a page by its handle ID.
type PageDestroyer func(id int64)

// AdaptivePool manages a pool of page handles with automatic scaling
// based on memory pressure and utilization.
type AdaptivePool struct {
	cfg       AdaptivePoolConfig
	factory   PageFactory
	destroyer PageDestroyer

	idle    chan *PageHandle
	mu      sync.Mutex
	all     map[int64]*PageHandle // all live handles
	nextID  atomic.Int64
	active  atomic.Int32 // currently checked-out handles
	stopped chan struct{}
}

// NewAdaptivePool creates and starts an adaptive pool.
// It pre-creates minPages handles using the factory.
func NewAdaptivePool(cfg AdaptivePoolConfig, factory PageFactory, destroyer PageDestroyer) (*AdaptivePool, error) {
	if cfg.MinPages < 1 {
		cfg.MinPages = 1
	}
	if cfg.HardMax < cfg.MinPages {
		cfg.HardMax = cfg.MinPages
	}
	if cfg.MemThreshold <= 0 {
		cfg.MemThreshold = 0.9
	}
	if cfg.ScaleStep <= 0 {
		cfg.ScaleStep = 0.05
	}

	ap := &AdaptivePool{
		cfg:       cfg,
		factory:   factory,
		destroyer: destroyer,
		idle:      make(chan *PageHandle, cfg.HardMax),
		all:       make(map[int64]*PageHandle),
		stopped:   make(chan struct{}),
	}

	// Pre-create minimum pages.
	for i := 0; i < cfg.MinPages; i++ {
		h, err := ap.createHandle()
		if err != nil {
			slog.Warn("adaptive_pool: failed to pre-create page", "error", err)
			continue
		}
		ap.idle <- h
	}

	go ap.scalingLoop()
	return ap, nil
}

// Get acquires a page handle from the pool. It blocks until one is available
// or creates a new one if under the hard max.
func (ap *AdaptivePool) Get() (*PageHandle, error) {
	// Try non-blocking first.
	select {
	case h := <-ap.idle:
		ap.active.Add(1)
		return h, nil
	default:
	}

	// Try to create a new handle if under hard max.
	ap.mu.Lock()
	if len(ap.all) < ap.cfg.HardMax {
		h, err := ap.createHandleLocked()
		ap.mu.Unlock()
		if err == nil {
			ap.active.Add(1)
			return h, nil
		}
		// Fall through to blocking wait.
	} else {
		ap.mu.Unlock()
	}

	// Block until one becomes available.
	h := <-ap.idle
	ap.active.Add(1)
	return h, nil
}

// Put returns a page handle to the pool. If the page should be retired,
// it is destroyed and a fresh one is created to replace it.
func (ap *AdaptivePool) Put(h *PageHandle, success bool) {
	ap.active.Add(-1)

	if success {
		h.RecordSuccess()
	} else {
		h.RecordFailure()
	}

	if h.ShouldRetire() {
		slog.Debug("adaptive_pool: retiring page", "id", h.ID,
			"errScore", h.errScore, "useCount", h.useCount)
		ap.destroyHandle(h)

		// Replace the retired page if we're at or below minimum.
		ap.mu.Lock()
		if len(ap.all) < ap.cfg.MinPages {
			if newH, err := ap.createHandleLocked(); err == nil {
				ap.mu.Unlock()
				ap.idle <- newH
				return
			}
		}
		ap.mu.Unlock()
		return
	}

	ap.idle <- h
}

// Size returns the total number of live handles.
func (ap *AdaptivePool) Size() int {
	ap.mu.Lock()
	defer ap.mu.Unlock()
	return len(ap.all)
}

// ActiveCount returns the number of currently checked-out handles.
func (ap *AdaptivePool) ActiveCount() int {
	return int(ap.active.Load())
}

// Stop shuts down the pool's scaling goroutine and destroys all handles.
func (ap *AdaptivePool) Stop() {
	close(ap.stopped)

	// Drain idle channel.
drainLoop:
	for {
		select {
		case h := <-ap.idle:
			ap.destroyHandle(h)
		default:
			break drainLoop
		}
	}

	// Destroy any remaining tracked handles.
	ap.mu.Lock()
	for id, h := range ap.all {
		ap.destroyer(h.ID)
		delete(ap.all, id)
	}
	ap.mu.Unlock()
}

// createHandle creates a new handle (acquires lock internally).
func (ap *AdaptivePool) createHandle() (*PageHandle, error) {
	ap.mu.Lock()
	defer ap.mu.Unlock()
	return ap.createHandleLocked()
}

// createHandleLocked creates a new handle. Caller must hold ap.mu.
func (ap *AdaptivePool) createHandleLocked() (*PageHandle, error) {
	id, err := ap.factory()
	if err != nil {
		return nil, err
	}
	h := NewPageHandle(id)
	ap.all[id] = h
	return h, nil
}

// destroyHandle removes a handle from tracking and calls the destroyer.
func (ap *AdaptivePool) destroyHandle(h *PageHandle) {
	ap.mu.Lock()
	delete(ap.all, h.ID)
	ap.mu.Unlock()
	ap.destroyer(h.ID)
}

// scalingLoop periodically samples memory and adjusts pool size.
func (ap *AdaptivePool) scalingLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ap.stopped:
			return
		case <-ticker.C:
			ap.scaleCheck()
		}
	}
}

func (ap *AdaptivePool) scaleCheck() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Estimate memory pressure as HeapInuse / HeapSys.
	var memPressure float64
	if m.HeapSys > 0 {
		memPressure = float64(m.HeapInuse) / float64(m.HeapSys)
	}

	ap.mu.Lock()
	totalSize := len(ap.all)
	ap.mu.Unlock()

	active := int(ap.active.Load())
	var activeRate float64
	if totalSize > 0 {
		activeRate = float64(active) / float64(totalSize)
	}

	if memPressure > ap.cfg.MemThreshold {
		// Shrink: close some idle pages.
		shrinkCount := int(math.Ceil(float64(totalSize) * ap.cfg.ScaleStep))
		for i := 0; i < shrinkCount; i++ {
			ap.mu.Lock()
			if len(ap.all) <= ap.cfg.MinPages {
				ap.mu.Unlock()
				break
			}
			ap.mu.Unlock()

			select {
			case h := <-ap.idle:
				slog.Debug("adaptive_pool: shrinking, retiring page", "id", h.ID)
				ap.destroyHandle(h)
			default:
				// No idle pages to shrink.
				return
			}
		}
	} else if activeRate > 0.8 {
		// Grow: add pages if under hard max.
		growCount := int(math.Ceil(float64(totalSize) * ap.cfg.ScaleStep))
		for i := 0; i < growCount; i++ {
			ap.mu.Lock()
			if len(ap.all) >= ap.cfg.HardMax {
				ap.mu.Unlock()
				break
			}
			h, err := ap.createHandleLocked()
			ap.mu.Unlock()
			if err != nil {
				slog.Warn("adaptive_pool: failed to grow", "error", err)
				break
			}
			slog.Debug("adaptive_pool: grew pool", "id", h.ID)
			ap.idle <- h
		}
	}
}
