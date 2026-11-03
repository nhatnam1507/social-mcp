// Package oauth tracks pending OAuth logins and implements PKCE helpers.
package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

const verifierAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"

// NewVerifier returns a 64-character PKCE code verifier.
func NewVerifier() string {
	b := make([]byte, 64)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = verifierAlphabet[int(b[i])%len(verifierAlphabet)]
	}
	return string(b)
}

// ChallengeS256Base64URL is the RFC 7636 S256 challenge.
func ChallengeS256Base64URL(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// ChallengeS256Hex is the hex-encoded SHA-256 challenge TikTok's desktop login expects.
func ChallengeS256Hex(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return hex.EncodeToString(sum[:])
}

func newState() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
