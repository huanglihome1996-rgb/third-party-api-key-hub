package callback

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type stateEntry struct {
	appID   string
	expires time.Time
}

// StateStore keeps only a hash of the browser-visible state. It is intentionally
// single-use so a callback cannot be replayed to exchange a second token.
type StateStore struct {
	mu      sync.Mutex
	ttl     time.Duration
	now     func() time.Time
	entries map[[32]byte]stateEntry
	path    string
}

type persistedState struct {
	Hash    string    `json:"hash"`
	AppID   string    `json:"app_id"`
	Expires time.Time `json:"expires"`
}

func NewStateStore(ttl time.Duration) *StateStore {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &StateStore{ttl: ttl, now: time.Now, entries: make(map[[32]byte]stateEntry)}
}

// NewFileStateStore persists only state hashes and their App binding. The
// browser-visible state itself is never written to disk.
func NewFileStateStore(path string, ttl time.Duration) (*StateStore, error) {
	if path == "" {
		return nil, errors.New("state store path is empty")
	}
	s := NewStateStore(ttl)
	s.path = path
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var stored []persistedState
	if err := json.Unmarshal(b, &stored); err != nil {
		return nil, errors.New("invalid state store")
	}
	for _, item := range stored {
		hash, err := hex.DecodeString(item.Hash)
		if err != nil || len(hash) != sha256.Size || item.AppID == "" {
			return nil, errors.New("invalid state store entry")
		}
		var key [sha256.Size]byte
		copy(key[:], hash)
		s.entries[key] = stateEntry{appID: item.AppID, expires: item.Expires}
	}
	return s, nil
}

func (s *StateStore) Issue(appID string) (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	state := base64.RawURLEncoding.EncodeToString(raw[:])
	s.mu.Lock()
	s.entries[sha256.Sum256([]byte(state))] = stateEntry{appID: appID, expires: s.now().Add(s.ttl)}
	if err := s.persistLocked(); err != nil {
		delete(s.entries, sha256.Sum256([]byte(state)))
		s.mu.Unlock()
		return "", err
	}
	s.mu.Unlock()
	return state, nil
}

func (s *StateStore) Consume(state string) (string, bool) {
	if state == "" {
		return "", false
	}
	now := s.now()
	key := sha256.Sum256([]byte(state))
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, entry := range s.entries {
		if !entry.expires.After(now) {
			delete(s.entries, k)
		}
	}
	entry, ok := s.entries[key]
	if !ok {
		return "", false
	}
	delete(s.entries, key)
	if err := s.persistLocked(); err != nil {
		s.entries[key] = entry
		return "", false
	}
	return entry.appID, true
}

func (s *StateStore) persistLocked() error {
	if s.path == "" {
		return nil
	}
	stored := make([]persistedState, 0, len(s.entries))
	for hash, entry := range s.entries {
		stored = append(stored, persistedState{Hash: hex.EncodeToString(hash[:]), AppID: entry.appID, Expires: entry.expires})
	}
	b, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".state-store-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(b)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("publish state store: %w", err)
	}
	return nil
}
