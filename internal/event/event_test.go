package event

import (
	"encoding/json"
	"testing"
)

// TestProviderFallbackTypeRoundTripsJSON proves the fallback audit contract
// type serializes like every other event type. It does not emit or implement
// any fallback; the type is a contract for future fallback code only.
func TestProviderFallbackTypeRoundTripsJSON(t *testing.T) {
	if string(ProviderFallback) != "provider_fallback" {
		t.Fatalf("ProviderFallback=%q", ProviderFallback)
	}
	original := New(ProviderFallback, "session-1")
	original.RunID = "run-1"
	original.MissionID = "mission-1"
	original.WorkItemID = "build"
	original.AgentID = "builder"
	original.Message = "role builder provider fallback anthropic->openai: rate limited"
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Event
	if err = json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Type != ProviderFallback {
		t.Fatalf("type=%q want %q", decoded.Type, ProviderFallback)
	}
	if decoded.RunID != original.RunID || decoded.MissionID != original.MissionID || decoded.WorkItemID != original.WorkItemID || decoded.AgentID != original.AgentID || decoded.Message != original.Message || !decoded.Time.Equal(original.Time) {
		t.Fatalf("round-trip mismatch: got %+v want %+v", decoded, original)
	}
}

// TestApprovalLifecycleTypesRoundTripJSON proves the approval contract event
// types (ADR 0011) serialize like every other event type. Nothing emits them
// from a real tool call yet; request production lands in C3.
func TestApprovalLifecycleTypesRoundTripJSON(t *testing.T) {
	want := map[Type]string{
		ApprovalRequested: "approval_requested",
		ApprovalDecided:   "approval_decided",
		ApprovalExpired:   "approval_expired",
		ApprovalConsumed:  "approval_consumed",
	}
	for eventType, name := range want {
		if string(eventType) != name {
			t.Fatalf("type=%q want %q", eventType, name)
		}
		original := New(eventType, "session-1")
		original.RunID = "run-1"
		original.Message = "apr-1"
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatal(err)
		}
		var decoded Event
		if err = json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.Type != eventType || decoded.RunID != original.RunID || decoded.Message != original.Message || !decoded.Time.Equal(original.Time) {
			t.Fatalf("round-trip mismatch: got %+v want %+v", decoded, original)
		}
	}
}
