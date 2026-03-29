package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Severity represents the urgency level of a notification.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityError    Severity = "error"
	SeverityWarning  Severity = "warning"
	SeverityInfo     Severity = "info"
)

// Alert contains the notification payload sent to external systems.
type Alert struct {
	Title       string
	Message     string
	Severity    Severity
	Namespace   string
	Resource    string
	Timestamp   time.Time
	ExtraFields map[string]string
}

// Sender dispatches alert notifications to an external system.
// Implementations must be safe for concurrent use.
type Sender interface {
	// Send dispatches the alert to the configured endpoint.
	Send(ctx context.Context, alert Alert) error

	// Name returns the sender type for logging (e.g. "Slack", "PagerDuty").
	Name() string
}

// SlackSender sends alerts to a Slack incoming webhook URL.
type SlackSender struct {
	webhookURL string
	channel    string
	httpClient *http.Client
}

// NewSlackSender creates a Sender that posts alerts to Slack via incoming webhook.
func NewSlackSender(webhookURL, channel string) Sender {
	return &SlackSender{
		webhookURL: webhookURL,
		channel:    channel,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *SlackSender) Name() string { return "Slack" }

// Send posts a formatted alert message to the Slack webhook.
func (s *SlackSender) Send(ctx context.Context, alert Alert) error {
	color := slackColor(alert.Severity)
	text := fmt.Sprintf("*%s*\n%s", alert.Title, alert.Message)
	if alert.Namespace != "" {
		text += fmt.Sprintf("\n*Namespace:* %s", alert.Namespace)
	}
	if alert.Resource != "" {
		text += fmt.Sprintf("\n*Resource:* %s", alert.Resource)
	}

	payload := map[string]interface{}{
		"attachments": []map[string]interface{}{
			{
				"color": color,
				"text":  text,
				"ts":    alert.Timestamp.Unix(),
			},
		},
	}
	if s.channel != "" {
		payload["channel"] = s.channel
	}

	return s.post(ctx, payload)
}

func (s *SlackSender) post(ctx context.Context, payload interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling slack payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating slack request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending slack notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("slack returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func slackColor(sev Severity) string {
	switch sev {
	case SeverityCritical:
		return "#FF0000"
	case SeverityError:
		return "#E01E5A"
	case SeverityWarning:
		return "#ECB22E"
	case SeverityInfo:
		return "#36C5F0"
	default:
		return "#808080"
	}
}

// PagerDutySender sends alerts to PagerDuty via the Events API v2.
type PagerDutySender struct {
	routingKey string
	severity   string
	httpClient *http.Client
}

// NewPagerDutySender creates a Sender that triggers PagerDuty incidents.
func NewPagerDutySender(routingKey, severity string) Sender {
	if severity == "" {
		severity = "warning"
	}
	return &PagerDutySender{
		routingKey: routingKey,
		severity:   severity,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *PagerDutySender) Name() string { return "PagerDuty" }

const pagerDutyEventsURL = "https://events.pagerduty.com/v2/enqueue"

// Send creates a PagerDuty event via the Events API v2.
func (p *PagerDutySender) Send(ctx context.Context, alert Alert) error {
	payload := map[string]interface{}{
		"routing_key":  p.routingKey,
		"event_action": "trigger",
		"payload": map[string]interface{}{
			"summary":  fmt.Sprintf("[ScalePilot] %s: %s", alert.Title, alert.Message),
			"severity": p.severity,
			"source":   "scalepilot-operator",
			"custom_details": map[string]string{
				"namespace": alert.Namespace,
				"resource":  alert.Resource,
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling pagerduty payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pagerDutyEventsURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating pagerduty request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending pagerduty notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("pagerduty returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
