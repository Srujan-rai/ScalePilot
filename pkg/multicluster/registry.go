package multicluster

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ClusterEntry holds a controller-runtime client and metadata for a single cluster.
type ClusterEntry struct {
	Name        string
	Client      client.Client
	Config      *rest.Config
	Healthy     bool
	LastProbe   time.Time
	LastError   error
	Namespace   string // default target namespace for this cluster
	MaxCapacity *int32
	Priority    int32
}

// HealthChecker tests whether a cluster's API server is reachable.
type HealthChecker interface {
	CheckHealth(ctx context.Context, config *rest.Config) error
}

// Registry manages controller-runtime clients for multiple clusters.
// All methods are safe for concurrent use.
type Registry interface {
	// Register adds or updates a cluster entry from a raw kubeconfig.
	Register(ctx context.Context, name string, kubeconfig []byte, scheme *runtime.Scheme) error

	// Unregister removes a cluster from the registry.
	Unregister(name string)

	// Get returns the ClusterEntry for the given cluster name.
	Get(name string) (*ClusterEntry, bool)

	// List returns all registered clusters, sorted by priority.
	List() []*ClusterEntry

	// HealthyOverflow returns overflow clusters that are healthy,
	// sorted by priority (lowest first).
	HealthyOverflow() []*ClusterEntry

	// RunHealthChecks probes all registered clusters and updates
	// their health status. Intended to be called periodically.
	RunHealthChecks(ctx context.Context)
}

// clusterRegistry is the concrete implementation of Registry.
type clusterRegistry struct {
	mu       sync.RWMutex
	clusters map[string]*ClusterEntry
	checker  HealthChecker
}

// NewRegistry creates a new cluster registry with the provided health checker.
func NewRegistry(checker HealthChecker) Registry {
	return &clusterRegistry{
		clusters: make(map[string]*ClusterEntry),
		checker:  checker,
	}
}

// Register adds or replaces a cluster entry by parsing the provided kubeconfig
// bytes and constructing a controller-runtime client.
func (r *clusterRegistry) Register(ctx context.Context, name string, kubeconfig []byte, scheme *runtime.Scheme) error {
	cfg, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		return fmt.Errorf("parsing kubeconfig for cluster %s: %w", name, err)
	}

	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("creating client for cluster %s: %w", name, err)
	}

	entry := &ClusterEntry{
		Name:      name,
		Client:    c,
		Config:    cfg,
		Healthy:   true,
		LastProbe: time.Now(),
	}

	r.mu.Lock()
	r.clusters[name] = entry
	r.mu.Unlock()

	return nil
}

// Unregister removes a cluster from the registry.
func (r *clusterRegistry) Unregister(name string) {
	r.mu.Lock()
	delete(r.clusters, name)
	r.mu.Unlock()
}

// Get returns the ClusterEntry for the given name.
func (r *clusterRegistry) Get(name string) (*ClusterEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.clusters[name]
	if !ok {
		return nil, false
	}
	// Return a shallow copy to avoid data races on mutable fields.
	copied := *entry
	return &copied, true
}

// List returns all registered clusters sorted by priority.
func (r *clusterRegistry) List() []*ClusterEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entries := make([]*ClusterEntry, 0, len(r.clusters))
	for _, e := range r.clusters {
		copied := *e
		entries = append(entries, &copied)
	}
	sortByPriority(entries)
	return entries
}

// HealthyOverflow returns healthy clusters sorted by priority.
func (r *clusterRegistry) HealthyOverflow() []*ClusterEntry {
	all := r.List()
	healthy := make([]*ClusterEntry, 0, len(all))
	for _, e := range all {
		if e.Healthy {
			healthy = append(healthy, e)
		}
	}
	return healthy
}

// RunHealthChecks probes every registered cluster and updates health status.
func (r *clusterRegistry) RunHealthChecks(ctx context.Context) {
	r.mu.RLock()
	names := make([]string, 0, len(r.clusters))
	configs := make(map[string]*rest.Config, len(r.clusters))
	for name, entry := range r.clusters {
		names = append(names, name)
		configs[name] = entry.Config
	}
	r.mu.RUnlock()

	type result struct {
		name string
		err  error
	}

	results := make(chan result, len(names))
	var wg sync.WaitGroup
	for _, name := range names {
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			err := r.checker.CheckHealth(ctx, configs[n])
			results <- result{name: n, err: err}
		}(name)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	for res := range results {
		r.mu.Lock()
		if entry, ok := r.clusters[res.name]; ok {
			entry.Healthy = res.err == nil
			entry.LastProbe = time.Now()
			entry.LastError = res.err
		}
		r.mu.Unlock()
	}
}

// sortByPriority sorts cluster entries by priority (ascending).
func sortByPriority(entries []*ClusterEntry) {
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && entries[j].Priority < entries[j-1].Priority; j-- {
			entries[j], entries[j-1] = entries[j-1], entries[j]
		}
	}
}
