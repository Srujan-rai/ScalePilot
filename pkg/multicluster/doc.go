// Package multicluster provides a cluster registry that manages
// controller-runtime clients for multiple Kubernetes clusters.
// Kubeconfig credentials are loaded from Secrets labeled
// scalepilot.io/cluster=true. Cluster health is checked periodically
// and all access to the registry is protected by sync.RWMutex for
// safe concurrent use from reconciler goroutines.
package multicluster
