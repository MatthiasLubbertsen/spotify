// Package crypto encrypts secrets (upstream client secret, refresh tokens) at rest.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type Box struct{ gcm cipher.AEAD }

// New derives an AES-256-GCM key from secret. When secret is empty a random key
// is generated once and persisted in dataDir/secret.key.
func New(secret, dataDir string) (*Box, error) {
	if secret == "" {
		p := filepath.Join(dataDir, "secret.key")
		b, err := os.ReadFile(p)
		if err != nil {
			raw := make([]byte, 32)
			if _, err := rand.Read(raw); err != nil {
				return nil, err
			}
			b = []byte(hex.EncodeToString(raw))
			if err := os.WriteFile(p, b, 0o600); err != nil {
				return nil, err
			}
		}
		secret = strings.TrimSpace(string(b))
	}
	sum := sha256.Sum256([]byte(secret))
	blk, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(blk)
	if err != nil {
		return nil, err
	}
	return &Box{gcm: g}, nil
}

func (b *Box) Seal(s string) (string, error) {
	nonce := make([]byte, b.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b.gcm.Seal(nonce, nonce, []byte(s), nil)), nil
}

func (b *Box) Open(s string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return "", err
	}
	n := b.gcm.NonceSize()
	if len(raw) < n {
		return "", errors.New("ciphertext too short")
	}
	out, err := b.gcm.Open(nil, raw[:n], raw[n:], nil)
	return string(out), err
}
