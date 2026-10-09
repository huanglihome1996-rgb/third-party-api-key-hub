package protector

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Fixture is ONLY a local demonstration backend. Production requires OpenBao.
// A value prefix is a fixture convention, never proof that a value is fictional.
type Fixture struct{ aead cipher.AEAD }

func NewFixture(keyPath string) (*Fixture, error) {
	key, err := os.ReadFile(keyPath)
	if errors.Is(err, os.ErrNotExist) {
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		f, err := os.CreateTemp(filepath.Dir(keyPath), ".fixture-key-*")
		if err != nil {
			return nil, err
		}
		defer os.Remove(f.Name())
		_, writeErr := f.Write(key)
		if writeErr == nil {
			writeErr = f.Sync()
		}
		closeErr := f.Close()
		if writeErr != nil {
			return nil, writeErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		// Publish the complete synced key atomically without replacing an existing key.
		if err = os.Link(f.Name(), keyPath); err != nil {
			if !errors.Is(err, os.ErrExist) {
				return nil, err
			}
			key, err = os.ReadFile(keyPath)
			if err != nil {
				return nil, err
			}
		}
	} else if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("invalid fixture key")
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	a, err := cipher.NewGCM(b)
	if err != nil {
		return nil, err
	}
	return &Fixture{aead: a}, nil
}
func binding(purpose, target, typ string, generation int64) []byte {
	raw, _ := json.Marshal([]any{1, purpose, target, typ, generation})
	return raw
}
func (f *Fixture) Seal(purpose, target, typ, value string, generation int64) (string, error) {
	if purpose != "current-credential" || generation <= 0 || (!strings.HasPrefix(value, "fixture-") && !strings.HasPrefix(value, "fake-")) {
		return "", errors.New("fixture protection rejected")
	}
	nonce := make([]byte, f.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := f.aead.Seal(nonce, nonce, []byte(value), binding(purpose, target, typ, generation))
	return base64.RawStdEncoding.EncodeToString(sealed), nil
}
func (f *Fixture) Open(purpose, target, typ, sealed string, generation int64) (string, error) {
	if purpose != "current-credential" || generation <= 0 {
		return "", errors.New("fixture protection rejected")
	}
	b, err := base64.RawStdEncoding.DecodeString(sealed)
	if err != nil || len(b) < f.aead.NonceSize() {
		return "", errors.New("invalid protected fixture")
	}
	nonce := b[:f.aead.NonceSize()]
	value, err := f.aead.Open(nil, nonce, b[f.aead.NonceSize():], binding(purpose, target, typ, generation))
	if err != nil {
		return "", errors.New("fixture binding rejected")
	}
	return string(value), nil
}
