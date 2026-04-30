package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type persistentBinding struct {
	AccountID  string    `json:"account_id"`
	BoundAt    time.Time `json:"bound_at"`
	LastUsedAt time.Time `json:"last_used_at"`
}

type persistentState struct {
	SessionBindings map[string]persistentBinding `json:"session_bindings"`
	AccountTokens   map[string]tokenSnapshot     `json:"account_tokens"`
}

type tokenSnapshot struct {
	SNlM0e    string    `json:"snlm0e"`
	BLToken   string    `json:"bl_token"`
	FSID      string    `json:"fsid"`
	ReqID     int64     `json:"req_id"`
	FetchedAt time.Time `json:"fetched_at"`
}

type stateStore struct {
	path string
	mu   sync.Mutex
}

func newStateStore(configPath string) *stateStore {
	return &stateStore{path: filepath.Join(filepath.Dir(configPath), "state.json")}
}

func (s *stateStore) load() (persistentState, error) {
	state := persistentState{SessionBindings: map[string]persistentBinding{}, AccountTokens: map[string]tokenSnapshot{}}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return state, err
	}
	if len(data) == 0 {
		return state, nil
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return persistentState{SessionBindings: map[string]persistentBinding{}, AccountTokens: map[string]tokenSnapshot{}}, err
	}
	if state.SessionBindings == nil {
		state.SessionBindings = map[string]persistentBinding{}
	}
	if state.AccountTokens == nil {
		state.AccountTokens = map[string]tokenSnapshot{}
	}
	return state, nil
}

func (s *stateStore) save(state persistentState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}
