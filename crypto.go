package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
)

var b64 = base64.RawURLEncoding

func randBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

func randHex(n int) string { return hex.EncodeToString(randBytes(n)) }
func randB64(n int) string { return b64.EncodeToString(randBytes(n)) }

// hashToken is used for bearer tokens and codes: they are high entropy, so a
// plain SHA-256 is enough and lets us look them up by hash.
func hashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

func (s *Server) seal(plain string) string {
	block, _ := aes.NewCipher(s.cfg.Key[:])
	gcm, _ := cipher.NewGCM(block)
	nonce := randBytes(gcm.NonceSize())
	return b64.EncodeToString(gcm.Seal(nonce, nonce, []byte(plain), nil))
}

func (s *Server) open(enc string) (string, error) {
	if enc == "" {
		return "", nil
	}
	raw, err := b64.DecodeString(enc)
	if err != nil {
		return "", err
	}
	block, _ := aes.NewCipher(s.cfg.Key[:])
	gcm, _ := cipher.NewGCM(block)
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	out, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	return string(out), err
}
