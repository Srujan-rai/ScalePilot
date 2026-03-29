package multicluster

import (
	"context"
	"fmt"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
)

// apiServerChecker implements HealthChecker by calling the /version
// endpoint on the target cluster's API server.
type apiServerChecker struct{}

// NewAPIServerHealthChecker creates a HealthChecker that validates cluster
// connectivity by querying the Kubernetes API server version endpoint.
func NewAPIServerHealthChecker() HealthChecker {
	return &apiServerChecker{}
}

// CheckHealth verifies that the API server at the given config is reachable
// by making a lightweight discovery request.
func (c *apiServerChecker) CheckHealth(ctx context.Context, config *rest.Config) error {
	// Use a short timeout so health checks don't block the reconciler.
	shortCfg := rest.CopyConfig(config)
	shortCfg.Timeout = 5_000_000_000 // 5 seconds in nanoseconds

	dc, err := discovery.NewDiscoveryClientForConfig(shortCfg)
	if err != nil {
		return fmt.Errorf("creating discovery client: %w", err)
	}

	_, err = dc.ServerVersion()
	if err != nil {
		return fmt.Errorf("querying server version: %w", err)
	}

	return nil
}
