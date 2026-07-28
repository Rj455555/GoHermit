package session

import "testing"

func TestStableRunIDIsIdempotentAndNeverCreatesReplacement(t *testing.T) {
	value, err := NewConversation("Task", t.TempDir(), "digest", Selection{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := value.NewRunWithID("run-stable-a", "Execute once")
	if err != nil {
		t.Fatal(err)
	}
	again, err := value.NewRunWithID("run-stable-a", "Execute once")
	if err != nil || again.ID != first.ID || len(value.Runs) != 1 {
		t.Fatalf("idempotent stable Run = %#v, %v; Runs=%#v", again, err, value.Runs)
	}
	if _, err = value.NewRunWithID("run-stable-a", "Different input"); err == nil {
		t.Fatal("stable Run accepted different input")
	}
	if _, err = value.NewRunWithID("../outside", "Execute once"); err == nil {
		t.Fatal("stable Run accepted path traversal ID")
	}
	if _, err = value.NewRunWithID("run-stable-b", "Execute again"); err == nil {
		t.Fatal("active Session created a replacement Run")
	}
}
