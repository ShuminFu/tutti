package agentstatus

import "sync"

// TerminalOperationStore retains the latest completed action for each provider.
// Tokens share active-action generations, so a superseded run cannot overwrite
// the result of a newer action.
type TerminalOperationStore struct {
	mu         sync.RWMutex
	byProvider map[string]terminalOperationEntry
}

type terminalOperationEntry struct {
	token  uint64
	result *RunActionResult
}

func NewTerminalOperationStore() *TerminalOperationStore {
	return &TerminalOperationStore{byProvider: map[string]terminalOperationEntry{}}
}

func (s *TerminalOperationStore) Begin(provider string, token uint64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.byProvider[provider] = terminalOperationEntry{token: token}
	s.mu.Unlock()
}

func (s *TerminalOperationStore) Complete(provider string, token uint64, result RunActionResult) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.byProvider[provider]
	if !ok || entry.token != token {
		return false
	}
	copy := result
	entry.result = &copy
	s.byProvider[provider] = entry
	return true
}

func (s *TerminalOperationStore) Latest(provider string) *RunActionResult {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.byProvider[provider]
	if !ok || entry.result == nil {
		return nil
	}
	copy := *entry.result
	return &copy
}
