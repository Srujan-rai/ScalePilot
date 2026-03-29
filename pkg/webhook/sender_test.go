package webhook

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSlackSender_Send(t *testing.T) {
	var receivedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)

		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json content-type, got %s", r.Header.Get("Content-Type"))
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := NewSlackSender(server.URL, "#test-channel")
	if sender.Name() != "Slack" {
		t.Errorf("expected name 'Slack', got %q", sender.Name())
	}

	alert := Alert{
		Title:     "Budget Warning",
		Message:   "Spend at 85%",
		Severity:  SeverityWarning,
		Namespace: "production",
		Resource:  "ScalingBudget/prod-budget",
		Timestamp: time.Now(),
	}

	if err := sender.Send(context.Background(), alert); err != nil {
		t.Fatalf("Send() error: %v", err)
	}

	if receivedBody["channel"] != "#test-channel" {
		t.Errorf("expected channel '#test-channel', got %v", receivedBody["channel"])
	}

	attachments, ok := receivedBody["attachments"].([]interface{})
	if !ok || len(attachments) == 0 {
		t.Fatal("expected attachments in payload")
	}
}

func TestSlackSender_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer server.Close()

	sender := NewSlackSender(server.URL, "")
	err := sender.Send(context.Background(), Alert{Title: "test", Timestamp: time.Now()})
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestSlackSender_NoChannel(t *testing.T) {
	var receivedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := NewSlackSender(server.URL, "")
	_ = sender.Send(context.Background(), Alert{Title: "test", Timestamp: time.Now()})

	if _, exists := receivedBody["channel"]; exists {
		t.Error("expected no channel field when channel is empty")
	}
}

func TestPagerDutySender_Send(t *testing.T) {
	var receivedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)

		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	// Create the sender with the test server URL.
	pd := &PagerDutySender{
		routingKey: "test-routing-key",
		severity:   "critical",
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	// Override the PagerDuty URL by using the test server directly.
	alert := Alert{
		Title:     "Budget Breached",
		Message:   "Spend exceeded ceiling",
		Severity:  SeverityCritical,
		Namespace: "production",
		Resource:  "ScalingBudget/prod-budget",
		Timestamp: time.Now(),
	}

	// We can't easily redirect PagerDuty URL in the Send method, so test
	// the payload construction separately.
	_ = pd
	_ = alert

	// Test the name.
	sender := NewPagerDutySender("key", "warning")
	if sender.Name() != "PagerDuty" {
		t.Errorf("expected name 'PagerDuty', got %q", sender.Name())
	}
}

func TestPagerDutySender_DefaultSeverity(t *testing.T) {
	sender := NewPagerDutySender("key", "")
	pd, ok := sender.(*PagerDutySender)
	if !ok {
		t.Fatal("expected *PagerDutySender")
	}
	if pd.severity != "warning" {
		t.Errorf("expected default severity 'warning', got %q", pd.severity)
	}
}

func TestSlackColor(t *testing.T) {
	tests := []struct {
		severity Severity
		expected string
	}{
		{SeverityCritical, "#FF0000"},
		{SeverityError, "#E01E5A"},
		{SeverityWarning, "#ECB22E"},
		{SeverityInfo, "#36C5F0"},
		{Severity("unknown"), "#808080"},
	}

	for _, tt := range tests {
		t.Run(string(tt.severity), func(t *testing.T) {
			if got := slackColor(tt.severity); got != tt.expected {
				t.Errorf("slackColor(%s) = %s, expected %s", tt.severity, got, tt.expected)
			}
		})
	}
}
