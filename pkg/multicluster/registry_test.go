package multicluster

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"k8s.io/client-go/rest"
)

// mockHealthChecker is a controllable HealthChecker for tests.
type mockHealthChecker struct {
	mu      sync.Mutex
	healthy map[string]bool
}

func newMockHealthChecker() *mockHealthChecker {
	return &mockHealthChecker{healthy: make(map[string]bool)}
}

func (m *mockHealthChecker) SetHealthy(host string, healthy bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.healthy[host] = healthy
}

func (m *mockHealthChecker) CheckHealth(_ context.Context, config *rest.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.healthy[config.Host]
	if !ok || !h {
		return fmt.Errorf("unhealthy")
	}
	return nil
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	checker := newMockHealthChecker()
	reg := NewRegistry(checker)

	entry, found := reg.Get("cluster-a")
	if found {
		t.Error("expected cluster-a not found before registration")
	}
	if entry != nil {
		t.Error("expected nil entry before registration")
	}
}

func TestRegistry_Unregister(t *testing.T) {
	checker := newMockHealthChecker()
	reg := NewRegistry(checker)

	cr, ok := reg.(*clusterRegistry)
	if !ok {
		t.Fatal("expected *clusterRegistry")
	}
	cr.mu.Lock()
	cr.clusters["test"] = &ClusterEntry{Name: "test", Healthy: true, Priority: 0}
	cr.mu.Unlock()

	_, found := reg.Get("test")
	if !found {
		t.Fatal("expected test cluster to be registered")
	}

	reg.Unregister("test")

	_, found = reg.Get("test")
	if found {
		t.Error("expected test cluster to be unregistered")
	}
}

func TestRegistry_List_SortedByPriority(t *testing.T) {
	checker := newMockHealthChecker()
	reg := NewRegistry(checker)

	cr, ok := reg.(*clusterRegistry)
	if !ok {
		t.Fatal("expected *clusterRegistry")
	}
	cr.mu.Lock()
	cr.clusters["high"] = &ClusterEntry{Name: "high", Priority: 10, Healthy: true}
	cr.clusters["low"] = &ClusterEntry{Name: "low", Priority: 1, Healthy: true}
	cr.clusters["mid"] = &ClusterEntry{Name: "mid", Priority: 5, Healthy: false}
	cr.mu.Unlock()

	list := reg.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(list))
	}
	if list[0].Name != "low" {
		t.Errorf("expected first entry 'low', got %q", list[0].Name)
	}
	if list[1].Name != "mid" {
		t.Errorf("expected second entry 'mid', got %q", list[1].Name)
	}
	if list[2].Name != "high" {
		t.Errorf("expected third entry 'high', got %q", list[2].Name)
	}
}

func TestRegistry_HealthyOverflow(t *testing.T) {
	checker := newMockHealthChecker()
	reg := NewRegistry(checker)

	cr, ok := reg.(*clusterRegistry)
	if !ok {
		t.Fatal("expected *clusterRegistry")
	}
	cr.mu.Lock()
	cr.clusters["healthy1"] = &ClusterEntry{Name: "healthy1", Priority: 2, Healthy: true}
	cr.clusters["unhealthy"] = &ClusterEntry{Name: "unhealthy", Priority: 1, Healthy: false}
	cr.clusters["healthy2"] = &ClusterEntry{Name: "healthy2", Priority: 3, Healthy: true}
	cr.mu.Unlock()

	healthy := reg.HealthyOverflow()
	if len(healthy) != 2 {
		t.Fatalf("expected 2 healthy entries, got %d", len(healthy))
	}
	for _, e := range healthy {
		if !e.Healthy {
			t.Errorf("entry %q should be healthy", e.Name)
		}
	}
}

func TestRegistry_RunHealthChecks(t *testing.T) {
	checker := newMockHealthChecker()
	reg := NewRegistry(checker)

	cr, ok := reg.(*clusterRegistry)
	if !ok {
		t.Fatal("expected *clusterRegistry")
	}
	cr.mu.Lock()
	cr.clusters["a"] = &ClusterEntry{
		Name:    "a",
		Config:  &rest.Config{Host: "host-a"},
		Healthy: true,
	}
	cr.clusters["b"] = &ClusterEntry{
		Name:    "b",
		Config:  &rest.Config{Host: "host-b"},
		Healthy: true,
	}
	cr.mu.Unlock()

	checker.SetHealthy("host-a", true)
	checker.SetHealthy("host-b", false)

	reg.RunHealthChecks(context.Background())

	a, _ := reg.Get("a")
	if !a.Healthy {
		t.Error("expected cluster 'a' to be healthy")
	}

	b, _ := reg.Get("b")
	if b.Healthy {
		t.Error("expected cluster 'b' to be unhealthy")
	}
	if b.LastError == nil {
		t.Error("expected cluster 'b' to have an error")
	}
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	checker := newMockHealthChecker()
	reg := NewRegistry(checker)

	cr, ok := reg.(*clusterRegistry)
	if !ok {
		t.Fatal("expected *clusterRegistry")
	}
	cr.mu.Lock()
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("cluster-%d", i)
		cr.clusters[name] = &ClusterEntry{
			Name:    name,
			Config:  &rest.Config{Host: name},
			Healthy: true,
		}
		checker.SetHealthy(name, i%2 == 0)
	}
	cr.mu.Unlock()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = reg.List()
			_ = reg.HealthyOverflow()
			reg.RunHealthChecks(context.Background())
		}()
	}
	wg.Wait()
}

func TestSortByPriority(t *testing.T) {
	entries := []*ClusterEntry{
		{Name: "c", Priority: 3},
		{Name: "a", Priority: 1},
		{Name: "b", Priority: 2},
	}
	sortByPriority(entries)
	if entries[0].Name != "a" || entries[1].Name != "b" || entries[2].Name != "c" {
		t.Errorf("unexpected order: %s, %s, %s", entries[0].Name, entries[1].Name, entries[2].Name)
	}
}
