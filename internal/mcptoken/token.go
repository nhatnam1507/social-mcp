// Package mcptoken manages the bearer token that MCP clients present to the daemon.
package mcptoken

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	if !errors.Is(err, os.ErrNotExist) {
		return tok, err
	}
	tmp, err := writeTemp(path)
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp)
	// Link fails if another process created the file first; either way, the file is the truth.
	err = os.Link(tmp, path)
	if err != nil && !errors.Is(err, os.ErrExist) {
		// No hard links here (FAT, some network mounts): create exclusively instead.
		data, rerr := os.ReadFile(tmp)
		if rerr != nil {
			return "", rerr
		}
		f, cerr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		switch {
		case cerr == nil:
			_, werr := f.Write(data)
			if err := errors.Join(werr, f.Close()); err != nil {
				return "", err
			}
		case !errors.Is(cerr, os.ErrExist):
			return "", cerr
		}
	}
	return Load(path)
}

// Rotate writes a new token atomically and returns it.
func Rotate(path string) (string, error) {
	tmp, err := writeTemp(path)
	if err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return Load(path)
}

// writeTemp writes a fresh token to a unique temp file next to path.
func writeTemp(path string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".mcp-token-*")
	if err != nil {
		return "", err
	}
	_, werr := f.WriteString(hex.EncodeToString(b) + "\n")
	cerr := f.Close()
	if err := errors.Join(werr, cerr, os.Chmod(f.Name(), 0o600)); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func valid(tok string) bool {
	if len(tok) != 64 {
		return false
	}
	_, err := hex.DecodeString(tok)
	return err == nil
}
