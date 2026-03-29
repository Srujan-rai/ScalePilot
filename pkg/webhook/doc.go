// Package webhook provides notification senders for Slack and PagerDuty.
// Both use simple HTTP POST calls against their respective webhook/event APIs
// rather than heavy SDK dependencies. Senders are safe for concurrent use.
package webhook
