package middleware

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MetricsRecorder observes per-method call outcomes.
// Implementations must be safe for concurrent use.
type MetricsRecorder interface {
	Observe(method string, latency time.Duration, err error)
}

// Metrics returns a middleware that records call count, latency, and error
// rate for every method through the provided recorder.
func Metrics(recorder MetricsRecorder) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			start := time.Now()
			result, err := next(ctx, method, req)
			recorder.Observe(method, time.Since(start), err)
			return result, err
		}
	}
}

// MethodStats holds accumulated statistics for a single method.
type MethodStats struct {
	Calls      int64
	Errors     int64
	TotalNanos int64 // cumulative latency in nanoseconds
}

// InMemoryRecorder is a thread-safe [MetricsRecorder] that stores per-method
// statistics in memory. Useful for testing and lightweight monitoring.
type InMemoryRecorder struct {
	mu    sync.RWMutex
	stats map[string]*methodCounters
}

// methodCounters holds atomics for a single method.
// The struct is allocated once per method and then accessed lock-free.
type methodCounters struct {
	calls      atomic.Int64
	errors     atomic.Int64
	totalNanos atomic.Int64
}

// NewInMemoryRecorder creates a ready-to-use in-memory recorder.
func NewInMemoryRecorder() *InMemoryRecorder {
	return &InMemoryRecorder{
		stats: make(map[string]*methodCounters),
	}
}

// Observe records a single method invocation.
func (r *InMemoryRecorder) Observe(method string, latency time.Duration, err error) {
	c := r.getOrCreate(method)
	c.calls.Add(1)
	c.totalNanos.Add(int64(latency))
	if err != nil {
		c.errors.Add(1)
	}
}

// Stats returns a snapshot of the accumulated statistics for a given method.
// Returns zero-valued MethodStats if the method was never observed.
func (r *InMemoryRecorder) Stats(method string) MethodStats {
	r.mu.RLock()
	c, ok := r.stats[method]
	r.mu.RUnlock()
	if !ok {
		return MethodStats{}
	}
	return MethodStats{
		Calls:      c.calls.Load(),
		Errors:     c.errors.Load(),
		TotalNanos: c.totalNanos.Load(),
	}
}

// getOrCreate returns the counters for method, allocating if necessary.
func (r *InMemoryRecorder) getOrCreate(method string) *methodCounters {
	// Fast path: already exists.
	r.mu.RLock()
	c, ok := r.stats[method]
	r.mu.RUnlock()
	if ok {
		return c
	}

	// Slow path: allocate under write lock.
	r.mu.Lock()
	defer r.mu.Unlock()
	// Double-check after acquiring write lock.
	if c, ok = r.stats[method]; ok {
		return c
	}
	c = &methodCounters{}
	r.stats[method] = c
	return c
}
