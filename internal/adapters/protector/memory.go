package protector

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// Protector is the boundary used by application code. Production must inject OpenBao.
type Protector interface {
	Seal(purpose, target, typ, value string, generation int64) (string, error)
	Open(purpose, target, typ, cipher string, generation int64) (string, error)
}

// Memory is a development-only protector for fictional fixtures; values are opaque and process-local.
type Memory struct {
	mu     sync.Mutex
	values map[string]string
	seq    uint64
}

func NewMemory() *Memory { return &Memory{values: map[string]string{}} }
func (m *Memory) Seal(purpose, target, typ, value string, g int64) (string, error) {
	if value == "" || strings.Contains(value, "real-") {
		return "", errors.New("development protector accepts fictional non-empty values only")
	}
	meta := purpose + "|" + target + "|" + typ + "|" + strconv.FormatInt(g, 10)
	nonce := atomic.AddUint64(&m.seq, 1)
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d", meta, nonce)))
	binding := sha256.Sum256([]byte(meta))
	key := hex.EncodeToString(binding[:]) + ":" + hex.EncodeToString(sum[:])
	m.mu.Lock()
	m.values[key] = value
	m.mu.Unlock()
	return key, nil
}
func (m *Memory) Open(purpose, target, typ, cipher string, g int64) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.values[cipher]
	if !ok {
		return "", errors.New("ciphertext unavailable")
	}
	sum := sha256.Sum256([]byte(purpose + "|" + target + "|" + typ + "|" + strconv.FormatInt(g, 10)))
	if !strings.HasPrefix(cipher, hex.EncodeToString(sum[:])+":") {
		return "", errors.New("binding mismatch")
	}
	return v, nil
}
