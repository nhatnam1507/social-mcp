// Package secrets manages the master key and seals tokens with AES-256-GCM.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

const sealVersion byte = 1

// Sealer encrypts and decrypts small secrets.
type Sealer struct {
	aead cipher.AEAD
}

// NewSealer returns a Sealer for a 32-byte key.
func NewSealer(key []byte) (*Sealer, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("master key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{aead: aead}, nil
}

// Seal returns version || nonce || ciphertext.
func (s *Sealer) Seal(plain []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append([]byte{sealVersion}, nonce...)
	return s.aead.Seal(out, nonce, plain, []byte{sealVersion}), nil
}

// Open reverses Seal.
func (s *Sealer) Open(sealed []byte) ([]byte, error) {
	ns := s.aead.NonceSize()
	if len(sealed) < 1+ns || sealed[0] != sealVersion {
		return nil, errors.New("sealed value has an unknown format")
	}
	plain, err := s.aead.Open(nil, sealed[1:1+ns], sealed[1+ns:], []byte{sealVersion})
	if err != nil {
		return nil, errors.New("sealed value cannot be decrypted with this key")
	}
	return plain, nil
}
