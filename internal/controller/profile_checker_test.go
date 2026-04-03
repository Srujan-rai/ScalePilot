package controller

import (
	"testing"
)

func TestScalePolicy_ShouldSuppress_Blackout(t *testing.T) {
	p := ScalePolicy{Blocked: true, BlackoutName: "friday-maintenance"}
	reason := p.ShouldSuppress()
	if reason == "" {
		t.Error("expected suppression reason for blackout")
	}
}

func TestScalePolicy_ShouldSuppress_GlobalDryRun(t *testing.T) {
	p := ScalePolicy{GlobalDryRun: true}
	reason := p.ShouldSuppress()
	if reason == "" {
		t.Error("expected suppression reason for global dry-run")
	}
}

func TestScalePolicy_ShouldSuppress_AllowedWhenClear(t *testing.T) {
	p := ScalePolicy{}
	reason := p.ShouldSuppress()
	if reason != "" {
		t.Errorf("expected no suppression, got %q", reason)
	}
}

func TestScalePolicy_BlackoutTakesPrecedence(t *testing.T) {
	p := ScalePolicy{Blocked: true, BlackoutName: "test", GlobalDryRun: true}
	reason := p.ShouldSuppress()
	if reason == "" {
		t.Error("expected suppression reason")
	}
	// Blackout should take precedence
	if reason != `blackout window "test" is active` {
		t.Errorf("expected blackout reason, got %q", reason)
	}
}
