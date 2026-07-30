package loopstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Rj455555/GoHermit/internal/loop"
	"github.com/Rj455555/GoHermit/internal/storage"
)

var ErrRuntimeStateNotFound = errors.New("loop runtime state not found")

func (s *Store) SaveRuntimeState(state loop.RuntimeState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateLoopPathID(state.LoopID); err != nil {
		return err
	}
	if err := loop.ValidateRuntimeState(state); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode loop runtime state: %w", err)
	}
	if len(raw) > MaxStoreBytes {
		return errors.New("loop runtime state exceeds size limit")
	}
	path := filepath.Join(s.dir, statesDir, state.LoopID+".json")
	if err = storage.AtomicWrite(path, append(raw, '\n'), 0600); err != nil {
		return fmt.Errorf("save loop runtime state: %w", err)
	}
	return nil
}

func (s *Store) GetRuntimeState(loopID string) (loop.RuntimeState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateLoopPathID(loopID); err != nil {
		return loop.RuntimeState{}, err
	}
	raw, err := os.ReadFile(filepath.Join(s.dir, statesDir, loopID+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return loop.RuntimeState{}, ErrRuntimeStateNotFound
	}
	if err != nil {
		return loop.RuntimeState{}, fmt.Errorf("read loop runtime state: %w", err)
	}
	if len(raw) > MaxStoreBytes {
		return loop.RuntimeState{}, errors.New("loop runtime state exceeds size limit")
	}
	var state loop.RuntimeState
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&state); err != nil {
		return loop.RuntimeState{}, fmt.Errorf("decode loop runtime state: %w", err)
	}
	var trailing any
	if err = decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return loop.RuntimeState{}, errors.New("loop runtime state contains trailing data")
	}
	if err = loop.ValidateRuntimeState(state); err != nil {
		return loop.RuntimeState{}, err
	}
	if state.LoopID != loopID {
		return loop.RuntimeState{}, errors.New("loop runtime state identity mismatch")
	}
	return state, nil
}
