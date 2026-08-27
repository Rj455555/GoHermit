package session

import (
	"context"
	"sync"
	"testing"
)

func TestReserveModelCallIsAtomicAndDurable(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root, ".gohermit")
	if err != nil {
		t.Fatal(err)
	}
	s, err := New("budget", root, "digest")
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.NewRun("budget")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), s); err != nil {
		t.Fatal(err)
	}

	const workers = 32
	var wait sync.WaitGroup
	wait.Add(workers)
	results := make(chan bool, workers)
	for range workers {
		go func() {
			defer wait.Done()
			reserved, reserveErr := store.ReserveModelCall(context.Background(), s, run.ID, 1)
			if reserveErr != nil {
				t.Errorf("reserve: %v", reserveErr)
				return
			}
			results <- reserved
		}()
	}
	wait.Wait()
	close(results)
	reserved := 0
	for result := range results {
		if result {
			reserved++
		}
	}
	if reserved != 1 || run.ModelCalls != 1 {
		t.Fatalf("reserved=%d run=%+v", reserved, *run)
	}
	loaded, err := store.Load(context.Background(), s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Runs[0].ModelCalls != 1 {
		t.Fatalf("persisted run=%+v", loaded.Runs[0])
	}
}
