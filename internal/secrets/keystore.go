package secrets

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zalando/go-keyring"
)

// Key backends recorded in the database.
const (
	BackendKeyring = "keyring"
	BackendFile    = "file"
)

const (
	keyringService = "social-mcp"
	metaBackend    = "key_backend"
	metaCheck      = "key_check"
	checkPlain     = "social-mcp-key-check"
)

var (
	// ErrKeyringNotFound means the keyring works but holds no entry.
	ErrKeyringNotFound = errors.New("secret not found in keyring")
	// ErrKeyringUnavailable means the keyring could not be reached in time.
	ErrKeyringUnavailable = errors.New("keyring unavailable")
)

// Keyring is the subset of an OS keyring the key store needs.
type Keyring interface {
	Get(service, user string) (string, error)
	Set(service, user, value string) error
}

// MetaStore persists the key backend and check value next to the sealed data.
type MetaStore interface {
	GetMeta(key string) (string, bool, error)
	SetMeta(key, value string) error
}

// Key is a loaded master key.
type Key struct {
	Bytes   []byte
	Backend string
}

// LoadKey returns the master key for home. A fresh home (no recorded key check) gets a new
// key in the keyring, or in home/master.key when the keyring is unavailable. An existing
// home loads only from its recorded backend and never creates a second key.
func LoadKey(home string, kr Keyring, meta MetaStore) (Key, error) {
	check, hasCheck, err := meta.GetMeta(metaCheck)
	if err != nil {
		return Key{}, err
	}
	if !hasCheck {
		return createKey(home, kr, meta)
	}
	backend, _, err := meta.GetMeta(metaBackend)
	if err != nil {
		return Key{}, err
	}
	var raw []byte
	switch backend {
	case BackendKeyring:
		v, err := kr.Get(keyringService, keyringUser(home))
		if err != nil {
			return Key{}, fmt.Errorf("load master key from the OS keyring: %w (tokens are sealed with that key; retry once the keyring is unlocked)", err)
		}
		if raw, err = hex.DecodeString(v); err != nil {
			return Key{}, fmt.Errorf("keyring entry is not a valid key: %w", err)
		}
	case BackendFile:
		if raw, err = readKeyFile(home); err != nil {
			return Key{}, fmt.Errorf("load master key file: %w", err)
		}
	default:
		return Key{}, fmt.Errorf("unknown key backend %q recorded in the database", backend)
	}
	if err := verifyCheck(raw, check); err != nil {
		return Key{}, err
	}
	return Key{Bytes: raw, Backend: backend}, nil
}

func createKey(home string, kr Keyring, meta MetaStore) (Key, error) {
	user := keyringUser(home)
	var k Key
	v, err := kr.Get(keyringService, user)
	switch {
	case err == nil:
		// Keyring already holds a key for this home (e.g. the database was deleted); reuse it.
		raw, derr := hex.DecodeString(v)
		if derr != nil || len(raw) != 32 {
			return Key{}, errors.New("existing keyring entry is not a valid key")
		}
		k = Key{Bytes: raw, Backend: BackendKeyring}
	case errors.Is(err, ErrKeyringNotFound):
		raw := newKeyBytes()
		if serr := kr.Set(keyringService, user, hex.EncodeToString(raw)); serr == nil {
			k = Key{Bytes: raw, Backend: BackendKeyring}
		} else if k, err = fileKey(home); err != nil {
			return Key{}, err
		}
	default:
		if k, err = fileKey(home); err != nil {
			return Key{}, err
		}
	}
	s, err := NewSealer(k.Bytes)
	if err != nil {
		return Key{}, err
	}
	sealed, err := s.Seal([]byte(checkPlain))
	if err != nil {
		return Key{}, err
	}
	if err := meta.SetMeta(metaBackend, k.Backend); err != nil {
		return Key{}, err
	}
	if err := meta.SetMeta(metaCheck, hex.EncodeToString(sealed)); err != nil {
		return Key{}, err
	}
	return k, nil
}

// fileKey reuses home/master.key or creates it.
func fileKey(home string) (Key, error) {
	raw, err := readKeyFile(home)
	if errors.Is(err, os.ErrNotExist) {
		raw = newKeyBytes()
		err = writeKeyFile(home, raw)
	}
	if err != nil {
		return Key{}, err
	}
	return Key{Bytes: raw, Backend: BackendFile}, nil
}

func verifyCheck(raw []byte, checkHex string) error {
	s, err := NewSealer(raw)
	if err != nil {
		return err
	}
	sealed, err := hex.DecodeString(checkHex)
	if err != nil {
		return errors.New("stored key check is corrupt")
	}
	plain, err := s.Open(sealed)
	if err != nil || string(plain) != checkPlain {
		return errors.New("master key does not match the one that sealed the stored tokens")
	}
	return nil
}

func keyFilePath(home string) string { return filepath.Join(home, "master.key") }

func readKeyFile(home string) ([]byte, error) {
	b, err := os.ReadFile(keyFilePath(home))
	if err != nil {
		return nil, err
	}
	raw, err := hex.DecodeString(string(b))
	if err != nil || len(raw) != 32 {
		return nil, errors.New("master.key is corrupt")
	}
	return raw, nil
}

func writeKeyFile(home string, raw []byte) error {
	tmp := keyFilePath(home) + ".tmp"
	if err := os.WriteFile(tmp, []byte(hex.EncodeToString(raw)), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, keyFilePath(home))
}

func newKeyBytes() []byte {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

// keyringUser scopes the keyring entry to one home directory.
func keyringUser(home string) string {
	sum := sha256.Sum256([]byte(home))
	return "master-key-" + hex.EncodeToString(sum[:])[:12]
}

// OSKeyring is the real keyring with a timeout, since Secret Service calls can hang.
type OSKeyring struct {
	Timeout time.Duration
}

// Get reads a keyring entry.
func (o OSKeyring) Get(service, user string) (string, error) {
	var v string
	err := o.withTimeout(func() error {
		var err error
		v, err = keyring.Get(service, user)
		return err
	})
	return v, err
}

// Set writes a keyring entry.
func (o OSKeyring) Set(service, user, value string) error {
	return o.withTimeout(func() error { return keyring.Set(service, user, value) })
}

func (o OSKeyring) withTimeout(fn func() error) error {
	timeout := o.Timeout
	if timeout == 0 {
		timeout = 3 * time.Second
	}
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		switch {
		case err == nil:
			return nil
		case errors.Is(err, keyring.ErrNotFound):
			return ErrKeyringNotFound
		default:
			return fmt.Errorf("%w: %v", ErrKeyringUnavailable, err)
		}
	case <-time.After(timeout):
		return fmt.Errorf("%w: timed out after %s", ErrKeyringUnavailable, timeout)
	}
}
