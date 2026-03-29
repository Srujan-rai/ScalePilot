// Package cloudcost provides adapters for querying cloud billing APIs
// (AWS Cost Explorer, GCP Billing, Azure Cost Management). Each provider
// implements the CostQuerier interface. API responses are cached in-memory
// with a configurable TTL (default 5 minutes) to avoid rate limiting.
package cloudcost
