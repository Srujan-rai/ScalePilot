package cloudcost

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// mockQuerier is a CostQuerier for testing.
type mockQuerier struct {
	mu        sync.Mutex
	callCount int
	data      *CostData
	err       error
}

func (m *mockQuerier) GetCurrentCost(_ context.Context, _ string) (*CostData, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++
	return m.data, m.err
}

func (m *mockQuerier) Provider() string { return "mock" }

func (m *mockQuerier) getCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

func TestCachedQuerier_CachesResults(t *testing.T) {
	inner := &mockQuerier{
		data: &CostData{
			CurrentSpendMillidollars: 50000,
			Currency:                 "USD",
			FetchedAt:                time.Now(),
		},
	}

	cached := NewCachedQuerier(inner, 5*time.Minute)

	// First call should hit the inner querier.
	data1, err := cached.GetCurrentCost(context.Background(), "production")
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}
	if data1.CurrentSpendMillidollars != 50000 {
		t.Errorf("expected 50000, got %d", data1.CurrentSpendMillidollars)
	}
	if inner.getCallCount() != 1 {
		t.Errorf("expected 1 inner call, got %d", inner.getCallCount())
	}

	// Second call should return cached data.
	data2, err := cached.GetCurrentCost(context.Background(), "production")
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}
	if data2.CurrentSpendMillidollars != 50000 {
		t.Errorf("expected 50000, got %d", data2.CurrentSpendMillidollars)
	}
	if inner.getCallCount() != 1 {
		t.Errorf("expected still 1 inner call after cache hit, got %d", inner.getCallCount())
	}
}

func TestCachedQuerier_DifferentNamespaces(t *testing.T) {
	inner := &mockQuerier{
		data: &CostData{CurrentSpendMillidollars: 10000},
	}

	cached := NewCachedQuerier(inner, 5*time.Minute)

	_, _ = cached.GetCurrentCost(context.Background(), "ns-a")
	_, _ = cached.GetCurrentCost(context.Background(), "ns-b")

	if inner.getCallCount() != 2 {
		t.Errorf("expected 2 inner calls for different namespaces, got %d", inner.getCallCount())
	}
}

func TestCachedQuerier_Invalidate(t *testing.T) {
	inner := &mockQuerier{
		data: &CostData{CurrentSpendMillidollars: 10000},
	}

	cached := NewCachedQuerier(inner, 5*time.Minute)

	_, _ = cached.GetCurrentCost(context.Background(), "prod")
	cached.Invalidate("prod")
	_, _ = cached.GetCurrentCost(context.Background(), "prod")

	if inner.getCallCount() != 2 {
		t.Errorf("expected 2 inner calls after invalidation, got %d", inner.getCallCount())
	}
}

func TestCachedQuerier_PropagatesErrors(t *testing.T) {
	inner := &mockQuerier{
		err: fmt.Errorf("API rate limited"),
	}

	cached := NewCachedQuerier(inner, 5*time.Minute)

	_, err := cached.GetCurrentCost(context.Background(), "prod")
	if err == nil {
		t.Error("expected error to be propagated")
	}
}

func TestCachedQuerier_DefaultTTL(t *testing.T) {
	inner := &mockQuerier{
		data: &CostData{CurrentSpendMillidollars: 1000},
	}

	cached := NewCachedQuerier(inner, 0)
	if cached.ttl != 5*time.Minute {
		t.Errorf("expected default TTL of 5m, got %v", cached.ttl)
	}
}

func TestCachedQuerier_Provider(t *testing.T) {
	inner := &mockQuerier{}
	cached := NewCachedQuerier(inner, time.Minute)
	if cached.Provider() != "mock" {
		t.Errorf("expected provider 'mock', got %q", cached.Provider())
	}
}

func TestCachedQuerier_ConcurrentAccess(t *testing.T) {
	inner := &mockQuerier{
		data: &CostData{CurrentSpendMillidollars: 5000},
	}

	cached := NewCachedQuerier(inner, 5*time.Minute)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = cached.GetCurrentCost(context.Background(), "prod")
		}()
	}
	wg.Wait()

	// The exact call count depends on timing, but it should be at least 1
	// and not crash (no data races).
	if inner.getCallCount() < 1 {
		t.Error("expected at least 1 inner call")
	}
}
