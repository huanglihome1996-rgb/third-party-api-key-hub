package callback

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

type encryptedStoreFile struct {
	Version    int    `json:"version"`
	Ciphertext string `json:"ciphertext"`
}

// EncryptedFileTokenSink is a small local fallback for deployments that do not
// yet wire OpenBao. It encrypts all token values with a separate 32-byte key
// file and publishes the ciphertext atomically with mode 0600.
type EncryptedFileTokenSink struct {
	mu   sync.Mutex
	path string
	aead cipher.AEAD
}

func NewEncryptedFileTokenSink(path, keyPath string) (*EncryptedFileTokenSink, error) {
	if path == "" || keyPath == "" || filepath.Clean(path) == filepath.Clean(keyPath) {
		return nil, errors.New("token store and key file must be separate paths")
	}
	keyInfo, err := os.Stat(keyPath)
	if err != nil {
		return nil, fmt.Errorf("token store key: %w", err)
	}
	if keyInfo.IsDir() || keyInfo.Mode().Perm()&0077 != 0 {
		return nil, errors.New("token store key must be a regular 0600 file")
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("token store key must contain exactly 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &EncryptedFileTokenSink{path: path, aead: aead}, nil
}

func (s *EncryptedFileTokenSink) Save(_ context.Context, appID string, token Token) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	values, err := s.readLocked()
	if err != nil {
		return err
	}
	values[appID] = token
	plain, err := json.Marshal(values)
	if err != nil {
		return err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	sealed := s.aead.Seal(nil, nonce, plain, nil)
	payload, err := json.Marshal(encryptedStoreFile{Version: 1, Ciphertext: base64.RawStdEncoding.EncodeToString(append(nonce, sealed...))})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".token-store-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(payload)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmpName, s.path)
}

func (s *EncryptedFileTokenSink) readLocked() (map[string]Token, error) {
	b, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return map[string]Token{}, nil
	}
	if err != nil {
		return nil, err
	}
	var envelope encryptedStoreFile
	if err := json.Unmarshal(b, &envelope); err != nil || envelope.Version != 1 {
		return nil, errors.New("invalid token store")
	}
	encoded, err := base64.RawStdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil || len(encoded) < s.aead.NonceSize() {
		return nil, errors.New("invalid token store ciphertext")
	}
	plain, err := s.aead.Open(nil, encoded[:s.aead.NonceSize()], encoded[s.aead.NonceSize():], nil)
	if err != nil {
		return nil, errors.New("cannot decrypt token store")
	}
	values := map[string]Token{}
	if err := json.Unmarshal(plain, &values); err != nil {
		return nil, errors.New("invalid token store values")
	}
	return values, nil
}
