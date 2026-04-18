package cloudcost

import (
	"context"
	"sync"
	"time"
)

// CostData holds the billing data for a namespace/account.
type CostData struct {
	// CurrentSpendMillidollars is the current period's spend in millidollars.
	CurrentSpendMillidollars int64

	// ForecastMillidollars is the projected end-of-period spend.
	ForecastMillidollars int64

	// PeriodStart is the beginning of the billing period.
	PeriodStart time.Time

	// PeriodEnd is the end of the billing period.
	PeriodEnd time.Time

	// Currency is the billing currency (e.g. "USD").
	Currency string

	// FetchedAt records when this data was retrieved.
	FetchedAt time.Time
}

// CostQuerier fetches billing data from a cloud provider.
// Implementations must be safe for concurrent use.
type CostQuerier interface {
	// GetCurrentCost returns the current billing period's spend.
	// The namespace parameter maps to cloud-specific cost allocation tags
	// or labels (e.g. AWS cost allocation tag, GCP label, Azure tag).
	GetCurrentCost(ctx context.Context, namespace string) (*CostData, error)

	// Provider returns the cloud provider name for logging.
	Provider() string
}

// CachedQuerier wraps a CostQuerier with in-memory TTL-based caching
// to prevent excessive API calls to billing endpoints.
type CachedQuerier struct {
	inner CostQuerier
	ttl   time.Duration

	mu    sync.RWMutex
	cache map[string]*cachedEntry
}

type cachedEntry struct {
	data      *CostData
	expiresAt time.Time
}

// NewCachedQuerier wraps a CostQuerier with a cache. Default TTL is 5 minutes.
func NewCachedQuerier(inner CostQuerier, ttl time.Duration) *CachedQuerier {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &CachedQuerier{
		inner: inner,
		ttl:   ttl,
		cache: make(map[string]*cachedEntry),
	}
}

// GetCurrentCost returns cached data if fresh, otherwise queries the underlying provider.
// A double-check under the write lock prevents concurrent goroutines from
// issuing duplicate backend queries when the cache entry expires simultaneously.
func (c *CachedQuerier) GetCurrentCost(ctx context.Context, namespace string) (*CostData, error) {
	c.mu.RLock()
	if entry, ok := c.cache[namespace]; ok && time.Now().Before(entry.expiresAt) {
		data := entry.data
		c.mu.RUnlock()
		return data, nil
	}
	c.mu.RUnlock()

	// Re-check under write lock so only one goroutine fetches for this namespace.
	c.mu.Lock()
	if entry, ok := c.cache[namespace]; ok && time.Now().Before(entry.expiresAt) {
		data := entry.data
		c.mu.Unlock()
		return data, nil
	}
	c.mu.Unlock()

	data, err := c.inner.GetCurrentCost(ctx, namespace)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.cache[namespace] = &cachedEntry{
		data:      data,
		expiresAt: time.Now().Add(c.ttl),
	}
	c.mu.Unlock()

	return data, nil
}

// Provider delegates to the inner querier.
func (c *CachedQuerier) Provider() string {
	return c.inner.Provider()
}

// Invalidate removes a namespace's cached data, forcing a fresh query.
func (c *CachedQuerier) Invalidate(namespace string) {
	c.mu.Lock()
	delete(c.cache, namespace)
	c.mu.Unlock()
}
