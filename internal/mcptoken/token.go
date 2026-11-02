// Package mcptoken manages the bearer token that MCP clients present to the daemon.
package mcptoken

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Load reads and validates the token at path.
func Load(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	tok := strings.TrimSpace(string(b))
	if !valid(tok) {
		return "", fmt.Errorf("%s is not a valid MCP token (expected 64 hex characters); run `social-mcp config mcp --rotate`", path)
	}
	return tok, nil
}

// LoadOrCreate returns the token at path, creating it if the file does not exist.
func LoadOrCreate(path string) (string, error) {
	tok, err := Load(path)
	if errors.Is(err, os.ErrNotExist) {
		return Rotate(path)
	}
	return tok, err
}

// Rotate writes a new token atomically and returns it.
func Rotate(path string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(b)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(tok+"\n"), 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	return tok, nil
}

func valid(tok string) bool {
	if len(tok) != 64 {
		return false
	}
	_, err := hex.DecodeString(tok)
	return err == nil
}
