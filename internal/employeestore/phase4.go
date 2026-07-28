package employeestore

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/Rj455555/GoHermit/internal/employeememory"
	"github.com/Rj455555/GoHermit/internal/knowledge"
)

type KnowledgeState struct {
	Sources []knowledge.Source `json:"sources"`
	Indexes []knowledge.Index  `json:"indexes"`
}

type knowledgeSourcesFile struct {
	SchemaVersion int                `json:"schema_version"`
	EmployeeID    string             `json:"employee_id"`
	Sources       []knowledge.Source `json:"sources"`
}

type knowledgeIndexFile struct {
	SchemaVersion int               `json:"schema_version"`
	EmployeeID    string            `json:"employee_id"`
	Indexes       []knowledge.Index `json:"indexes"`
}

type memoryFactsFile struct {
	SchemaVersion int                   `json:"schema_version"`
	EmployeeID    string                `json:"employee_id"`
	Facts         []employeememory.Fact `json:"facts"`
}

type memoryCandidatesFile struct {
	SchemaVersion int                        `json:"schema_version"`
	EmployeeID    string                     `json:"employee_id"`
	Candidates    []employeememory.Candidate `json:"candidates"`
}

func (s *Store) Knowledge(id string) (KnowledgeState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateStoreID(id); err != nil {
		return KnowledgeState{}, err
	}
	if _, err := s.getLockedWithoutMutex(id); err != nil {
		return KnowledgeState{}, err
	}
	return s.loadKnowledge(id)
}

// SaveKnowledge atomically replaces each individual Knowledge metadata file.
// It deliberately does not claim a cross-file transaction.
func (s *Store) SaveKnowledge(id string, source knowledge.Source, index knowledge.Index) (KnowledgeState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateStoreID(id); err != nil {
		return KnowledgeState{}, err
	}
	record, err := s.getLockedWithoutMutex(id)
	if err != nil {
		return KnowledgeState{}, err
	}
	if source.EmployeeID != id || index.EmployeeID != id {
		return KnowledgeState{}, errors.New("Knowledge identity mismatch")
	}
	if err := knowledge.ValidateSource(source, true); err != nil {
		return KnowledgeState{}, err
	}
	if err := knowledge.ValidateIndex(index, source); err != nil {
		return KnowledgeState{}, err
	}
	state, err := s.loadKnowledge(id)
	if err != nil {
		return KnowledgeState{}, err
	}
	sourceAt, indexAt := findKnowledgeSource(state.Sources, source.ID), findKnowledgeIndex(state.Indexes, source.ID)
	if sourceAt < 0 {
		if len(state.Sources) >= knowledge.MaxSources {
			return KnowledgeState{}, errors.New("Knowledge source limit reached")
		}
		state.Sources = append(state.Sources, source)
		state.Indexes = append(state.Indexes, index)
	} else {
		if indexAt < 0 {
			return KnowledgeState{}, fmt.Errorf("%w: Knowledge source has no index", ErrCorrupt)
		}
		state.Sources[sourceAt], state.Indexes[indexAt] = source, index
	}
	sort.Slice(state.Sources, func(i, j int) bool { return state.Sources[i].ID < state.Sources[j].ID })
	sort.Slice(state.Indexes, func(i, j int) bool { return state.Indexes[i].SourceID < state.Indexes[j].SourceID })
	if err := s.writeKnowledge(id, state); err != nil {
		return KnowledgeState{}, err
	}
	if err := s.appendActivity(id, ActivityEvent{
		EmployeeID: id, Type: ActivityKnowledgeBinding, EmployeeRevision: record.Employee.Revision,
		SubjectID: source.ID,
	}); err != nil {
		return KnowledgeState{}, err
	}
	return state, nil
}

func (s *Store) DeleteKnowledge(id, sourceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateStoreID(id); err != nil {
		return err
	}
	if err := validateStoreID(sourceID); err != nil {
		return errors.New("Knowledge source id is invalid")
	}
	record, err := s.getLockedWithoutMutex(id)
	if err != nil {
		return err
	}
	state, err := s.loadKnowledge(id)
	if err != nil {
		return err
	}
	sourceAt, indexAt := findKnowledgeSource(state.Sources, sourceID), findKnowledgeIndex(state.Indexes, sourceID)
	if sourceAt < 0 || indexAt < 0 {
		return knowledge.ErrMissing
	}
	state.Sources = append(state.Sources[:sourceAt], state.Sources[sourceAt+1:]...)
	state.Indexes = append(state.Indexes[:indexAt], state.Indexes[indexAt+1:]...)
	if err := s.writeKnowledge(id, state); err != nil {
		return err
	}
	return s.appendActivity(id, ActivityEvent{
		EmployeeID: id, Type: ActivityKnowledgeBinding, EmployeeRevision: record.Employee.Revision,
		SubjectID: sourceID,
	})
}

func (s *Store) loadKnowledge(id string) (KnowledgeState, error) {
	sources := knowledgeSourcesFile{SchemaVersion: knowledge.SchemaVersion, EmployeeID: id, Sources: []knowledge.Source{}}
	indexes := knowledgeIndexFile{SchemaVersion: knowledge.SchemaVersion, EmployeeID: id, Indexes: []knowledge.Index{}}
	if err := s.decodeFileStrict(knowledge.MaxIndexBytes, &sources, id, "knowledge", "sources.json"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return KnowledgeState{}, fmt.Errorf("%w: load Knowledge sources: %v", ErrCorrupt, err)
	}
	if err := s.decodeFileStrict(knowledge.MaxIndexBytes, &indexes, id, "knowledge", "index.json"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return KnowledgeState{}, fmt.Errorf("%w: load Knowledge index: %v", ErrCorrupt, err)
	}
	if sources.SchemaVersion != knowledge.SchemaVersion || indexes.SchemaVersion != knowledge.SchemaVersion ||
		sources.EmployeeID != id || indexes.EmployeeID != id || len(sources.Sources) > knowledge.MaxSources ||
		len(sources.Sources) != len(indexes.Indexes) {
		return KnowledgeState{}, fmt.Errorf("%w: Knowledge file identity or count", ErrCorrupt)
	}
	seen := map[string]struct{}{}
	for _, source := range sources.Sources {
		if source.EmployeeID != id {
			return KnowledgeState{}, fmt.Errorf("%w: Knowledge source identity", ErrCorrupt)
		}
		if err := knowledge.ValidateSource(source, true); err != nil {
			return KnowledgeState{}, fmt.Errorf("%w: %v", ErrCorrupt, err)
		}
		if _, duplicate := seen[source.ID]; duplicate {
			return KnowledgeState{}, fmt.Errorf("%w: duplicate Knowledge source", ErrCorrupt)
		}
		seen[source.ID] = struct{}{}
		position := findKnowledgeIndex(indexes.Indexes, source.ID)
		if position < 0 {
			return KnowledgeState{}, fmt.Errorf("%w: Knowledge index missing", ErrCorrupt)
		}
		if err := knowledge.ValidateIndex(indexes.Indexes[position], source); err != nil {
			return KnowledgeState{}, fmt.Errorf("%w: %v", ErrCorrupt, err)
		}
	}
	sort.Slice(sources.Sources, func(i, j int) bool { return sources.Sources[i].ID < sources.Sources[j].ID })
	sort.Slice(indexes.Indexes, func(i, j int) bool { return indexes.Indexes[i].SourceID < indexes.Indexes[j].SourceID })
	return KnowledgeState{Sources: sources.Sources, Indexes: indexes.Indexes}, nil
}

func (s *Store) writeKnowledge(id string, state KnowledgeState) error {
	indexes := knowledgeIndexFile{SchemaVersion: knowledge.SchemaVersion, EmployeeID: id, Indexes: state.Indexes}
	if err := s.writeJSON(indexes, knowledge.MaxIndexBytes, id, "knowledge", "index.json"); err != nil {
		return err
	}
	sources := knowledgeSourcesFile{SchemaVersion: knowledge.SchemaVersion, EmployeeID: id, Sources: state.Sources}
	return s.writeJSON(sources, knowledge.MaxIndexBytes, id, "knowledge", "sources.json")
}

func (s *Store) Memory(id string) ([]employeememory.Fact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateStoreID(id); err != nil {
		return nil, err
	}
	if _, err := s.getLockedWithoutMutex(id); err != nil {
		return nil, err
	}
	return s.loadFacts(id)
}

func (s *Store) MemoryCandidates(id string) ([]employeememory.Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateStoreID(id); err != nil {
		return nil, err
	}
	if _, err := s.getLockedWithoutMutex(id); err != nil {
		return nil, err
	}
	return s.loadCandidates(id)
}

// AddMemoryCandidate is a persistence seam for future verified runtime output
// and tests. Phase 4 intentionally exposes no HTTP endpoint that creates one.
func (s *Store) AddMemoryCandidate(id string, candidate employeememory.Candidate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateStoreID(id); err != nil {
		return err
	}
	if _, err := s.getLockedWithoutMutex(id); err != nil {
		return err
	}
	if candidate.EmployeeID != id {
		return errors.New("Memory Candidate identity mismatch")
	}
	if err := employeememory.ValidateCandidate(candidate); err != nil {
		return err
	}
	candidates, err := s.loadCandidates(id)
	if err != nil {
		return err
	}
	position := findCandidate(candidates, candidate.ID)
	if position >= 0 {
		if candidates[position].Digest == candidate.Digest {
			return nil
		}
		return ErrConflict
	}
	if len(candidates) >= employeememory.MaxCandidates {
		return errors.New("Memory Candidate limit reached")
	}
	candidates = append(candidates, candidate)
	employeememory.SortCandidates(candidates)
	return s.writeCandidates(id, candidates)
}

func (s *Store) AcceptMemoryCandidate(id, candidateID string) (employeememory.Fact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateStoreID(id); err != nil {
		return employeememory.Fact{}, err
	}
	if err := validateStoreID(candidateID); err != nil {
		return employeememory.Fact{}, errors.New("Memory Candidate id is invalid")
	}
	record, err := s.getLockedWithoutMutex(id)
	if err != nil {
		return employeememory.Fact{}, err
	}
	facts, err := s.loadFacts(id)
	if err != nil {
		return employeememory.Fact{}, err
	}
	for _, fact := range facts {
		if fact.CandidateID == candidateID {
			candidates, loadErr := s.loadCandidates(id)
			if loadErr != nil {
				return employeememory.Fact{}, loadErr
			}
			if position := findCandidate(candidates, candidateID); position >= 0 {
				candidates = append(candidates[:position], candidates[position+1:]...)
				if writeErr := s.writeCandidates(id, candidates); writeErr != nil {
					return employeememory.Fact{}, writeErr
				}
			}
			return fact, nil
		}
	}
	candidates, err := s.loadCandidates(id)
	if err != nil {
		return employeememory.Fact{}, err
	}
	position := findCandidate(candidates, candidateID)
	if position < 0 {
		return employeememory.Fact{}, employeememory.ErrMissing
	}
	if len(facts) >= employeememory.MaxFacts {
		return employeememory.Fact{}, errors.New("Employee Memory fact limit reached")
	}
	fact, err := employeememory.Promote(candidates[position], time.Now().UTC())
	if err != nil {
		return employeememory.Fact{}, err
	}
	facts = append(facts, fact)
	employeememory.SortFacts(facts)
	candidates = append(candidates[:position], candidates[position+1:]...)
	// The accepted fact is persisted before the Candidate disappears. A crash
	// between the two writes is recovered idempotently by CandidateID.
	if err := s.writeFacts(id, facts); err != nil {
		return employeememory.Fact{}, err
	}
	if err := s.writeCandidates(id, candidates); err != nil {
		return employeememory.Fact{}, err
	}
	if err := s.appendActivity(id, ActivityEvent{
		EmployeeID: id, Type: ActivityMemoryAccepted, EmployeeRevision: record.Employee.Revision,
		SubjectID: fact.ID,
	}); err != nil {
		return employeememory.Fact{}, err
	}
	return fact, nil
}

func (s *Store) RejectMemoryCandidate(id, candidateID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateStoreID(id); err != nil {
		return err
	}
	if err := validateStoreID(candidateID); err != nil {
		return errors.New("Memory Candidate id is invalid")
	}
	if _, err := s.getLockedWithoutMutex(id); err != nil {
		return err
	}
	candidates, err := s.loadCandidates(id)
	if err != nil {
		return err
	}
	position := findCandidate(candidates, candidateID)
	if position < 0 {
		return employeememory.ErrMissing
	}
	candidates = append(candidates[:position], candidates[position+1:]...)
	return s.writeCandidates(id, candidates)
}

func (s *Store) EditMemory(id, factID, value string) (employeememory.Fact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateStoreID(id); err != nil {
		return employeememory.Fact{}, err
	}
	if err := validateStoreID(factID); err != nil {
		return employeememory.Fact{}, errors.New("Memory fact id is invalid")
	}
	record, err := s.getLockedWithoutMutex(id)
	if err != nil {
		return employeememory.Fact{}, err
	}
	facts, err := s.loadFacts(id)
	if err != nil {
		return employeememory.Fact{}, err
	}
	position := findFact(facts, factID)
	if position < 0 {
		return employeememory.Fact{}, employeememory.ErrMissing
	}
	edited, err := employeememory.Edit(facts[position], value, time.Now().UTC())
	if err != nil {
		return employeememory.Fact{}, err
	}
	facts[position] = edited
	if err := s.writeFacts(id, facts); err != nil {
		return employeememory.Fact{}, err
	}
	if err := s.appendActivity(id, ActivityEvent{
		EmployeeID: id, Type: ActivityMemoryEdited, EmployeeRevision: record.Employee.Revision,
		SubjectID: edited.ID,
	}); err != nil {
		return employeememory.Fact{}, err
	}
	return edited, nil
}

func (s *Store) ForgetMemory(id, factID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateStoreID(id); err != nil {
		return err
	}
	if err := validateStoreID(factID); err != nil {
		return errors.New("Memory fact id is invalid")
	}
	record, err := s.getLockedWithoutMutex(id)
	if err != nil {
		return err
	}
	facts, err := s.loadFacts(id)
	if err != nil {
		return err
	}
	position := findFact(facts, factID)
	if position < 0 {
		return employeememory.ErrMissing
	}
	facts = append(facts[:position], facts[position+1:]...)
	if err := s.writeFacts(id, facts); err != nil {
		return err
	}
	return s.appendActivity(id, ActivityEvent{
		EmployeeID: id, Type: ActivityMemoryForgotten, EmployeeRevision: record.Employee.Revision,
		SubjectID: factID,
	})
}

func (s *Store) loadFacts(id string) ([]employeememory.Fact, error) {
	file := memoryFactsFile{SchemaVersion: employeememory.SchemaVersion, EmployeeID: id, Facts: []employeememory.Fact{}}
	if err := s.decodeFileStrict(employeememory.MaxFactFileBytes, &file, id, "memory", "facts.json"); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return file.Facts, nil
		}
		return nil, fmt.Errorf("%w: load Memory facts: %v", ErrCorrupt, err)
	}
	if file.SchemaVersion != employeememory.SchemaVersion || file.EmployeeID != id || len(file.Facts) > employeememory.MaxFacts {
		return nil, fmt.Errorf("%w: Memory facts identity or count", ErrCorrupt)
	}
	seen := map[string]struct{}{}
	for _, fact := range file.Facts {
		if fact.EmployeeID != id {
			return nil, fmt.Errorf("%w: Memory fact identity", ErrCorrupt)
		}
		if err := employeememory.ValidateFact(fact); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
		}
		if _, duplicate := seen[fact.ID]; duplicate {
			return nil, fmt.Errorf("%w: duplicate Memory fact", ErrCorrupt)
		}
		seen[fact.ID] = struct{}{}
	}
	employeememory.SortFacts(file.Facts)
	return file.Facts, nil
}

func (s *Store) loadCandidates(id string) ([]employeememory.Candidate, error) {
	file := memoryCandidatesFile{SchemaVersion: employeememory.SchemaVersion, EmployeeID: id, Candidates: []employeememory.Candidate{}}
	if err := s.decodeFileStrict(employeememory.MaxCandidateBytes, &file, id, "memory", "candidates.json"); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return file.Candidates, nil
		}
		return nil, fmt.Errorf("%w: load Memory Candidates: %v", ErrCorrupt, err)
	}
	if file.SchemaVersion != employeememory.SchemaVersion || file.EmployeeID != id || len(file.Candidates) > employeememory.MaxCandidates {
		return nil, fmt.Errorf("%w: Memory Candidate identity or count", ErrCorrupt)
	}
	seen := map[string]struct{}{}
	for _, candidate := range file.Candidates {
		if candidate.EmployeeID != id {
			return nil, fmt.Errorf("%w: Memory Candidate identity", ErrCorrupt)
		}
		if err := employeememory.ValidateCandidate(candidate); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
		}
		if _, duplicate := seen[candidate.ID]; duplicate {
			return nil, fmt.Errorf("%w: duplicate Memory Candidate", ErrCorrupt)
		}
		seen[candidate.ID] = struct{}{}
	}
	employeememory.SortCandidates(file.Candidates)
	return file.Candidates, nil
}

func (s *Store) writeFacts(id string, facts []employeememory.Fact) error {
	return s.writeJSON(memoryFactsFile{SchemaVersion: employeememory.SchemaVersion, EmployeeID: id, Facts: facts},
		employeememory.MaxFactFileBytes, id, "memory", "facts.json")
}

func (s *Store) writeCandidates(id string, candidates []employeememory.Candidate) error {
	return s.writeJSON(memoryCandidatesFile{SchemaVersion: employeememory.SchemaVersion, EmployeeID: id, Candidates: candidates},
		employeememory.MaxCandidateBytes, id, "memory", "candidates.json")
}

func findKnowledgeSource(values []knowledge.Source, id string) int {
	for index := range values {
		if values[index].ID == id {
			return index
		}
	}
	return -1
}

func findKnowledgeIndex(values []knowledge.Index, id string) int {
	for index := range values {
		if values[index].SourceID == id {
			return index
		}
	}
	return -1
}

func findCandidate(values []employeememory.Candidate, id string) int {
	for index := range values {
		if values[index].ID == id {
			return index
		}
	}
	return -1
}

func findFact(values []employeememory.Fact, id string) int {
	for index := range values {
		if values[index].ID == id {
			return index
		}
	}
	return -1
}
