package session

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Rj455555/GoHermit/internal/evals"
	"github.com/Rj455555/GoHermit/internal/event"
)

func TestRecoveryFixtures(t *testing.T) {
	fixture, err := evals.LoadFixture[evals.RecoveryFixture](filepath.Join("..", "evals", "testdata", "recovery.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range fixture.Scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			gradeRecoveryScenario(t, scenario)
		})
	}
}

// gradeRecoveryScenario crashes a commit at the fixture's journal stage, then
// recovers with fresh stores and asserts events survive exactly once and an
// interrupted run stays resumable across repeated recoveries.
func gradeRecoveryScenario(t *testing.T, scenario evals.RecoveryScenarioFixture) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	store, err := NewStore(root, ".gohermit")
	if err != nil {
		t.Fatal(err)
	}
	sess, err := New("recovery eval", root, "digest")
	if err != nil {
		t.Fatal(err)
	}
	if scenario.ActiveRun {
		if _, err = sess.NewRun("recoverable work"); err != nil {
			t.Fatal(err)
		}
	}
	if err = store.Save(ctx, sess); err != nil {
		t.Fatal(err)
	}
	crashed := false
	store.commitStageHook = func(stage string) error {
		if stage == scenario.CrashStage && !crashed {
			crashed = true
			return errors.New("simulated crash")
		}
		return nil
	}
	runtimeEvents := make([]event.Event, 0, scenario.Events)
	for i := 0; i < scenario.Events; i++ {
		runtimeEvents = append(runtimeEvents, event.New(event.PlanUpdated, sess.ID))
	}
	if _, err = store.CommitEvents(ctx, sess, runtimeEvents); err == nil {
		t.Fatal("expected simulated crash")
	}
	for recovery := 0; recovery < scenario.Recoveries; recovery++ {
		fresh, err := NewStore(root, ".gohermit")
		if err != nil {
			t.Fatal(err)
		}
		recovered, err := fresh.Recover(ctx, sess.ID)
		if err != nil {
			t.Fatalf("recovery %d: %v", recovery, err)
		}
		if recovered.NextEventSequence != uint64(scenario.Events) {
			t.Fatalf("recovery %d: next sequence=%d want %d", recovery, recovered.NextEventSequence, scenario.Events)
		}
		events, err := fresh.Events(sess.ID, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != scenario.Events {
			t.Fatalf("recovery %d: events=%d want %d (loss or duplication)", recovery, len(events), scenario.Events)
		}
		for i, runtimeEvent := range events {
			if want := uint64(i + 1); runtimeEvent.Sequence != want {
				t.Fatalf("recovery %d: event %d sequence=%d want %d", recovery, i, runtimeEvent.Sequence, want)
			}
		}
		if scenario.ActiveRun {
			active := recovered.ActiveRun()
			if active == nil || active.Status != RunInterrupted || recovered.ActiveRunID == "" {
				t.Fatalf("recovery %d: interrupted run not resumable: %+v", recovery, active)
			}
		}
	}
}
