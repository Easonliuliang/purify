package engine

// Page health scoring is integrated directly into PageHandle (adaptive_pool.go).
//
// Scoring rules:
//   - Success: errScore -= 0.5 (min 0)
//   - Failure: errScore += 1.0
//
// Retirement triggers (any one):
//   - errScore >= 3.0
//   - useCount >= 50
//   - age >= 50 minutes
//
// The AdaptivePool.Put(handle, success) method applies scoring and retires
// unhealthy pages automatically. See PageHandle.RecordSuccess(),
// PageHandle.RecordFailure(), and PageHandle.ShouldRetire() in adaptive_pool.go.
