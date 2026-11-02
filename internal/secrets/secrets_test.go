package secrets

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type fakeKeyring struct {
	data        map[string]string
	unavailable bool
	setFails    bool
}

func newFakeKeyring() *fakeKeyring { return &fakeKeyring{data: map[string]string{}} }

func (f *fakeKeyring) Get(service, user string) (string, error) {
	if f.unavailable {
		return "", ErrKeyringUnavailable
	}
	v, ok := f.data[service+"/"+user]
	if !ok {
		return "", ErrKeyringNotFound
	}
	return v, nil
}

func (f *fakeKeyring) Set(service, user, value string) error {
	if f.unavailable || f.setFails {
		return ErrKeyringUnavailable
	}
	f.data[service+"/"+user] = value
	return nil
}

type memMeta map[string]string

func (m memMeta) GetMeta(key string) (string, bool, error) { v, ok := m[key]; return v, ok, nil }
func (m memMeta) SetMeta(key, value string) error          { m[key] = value; return nil }

func TestSealerRoundTripAndTamper(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	s, err := NewSealer(key)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := s.Seal([]byte("token"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := s.Open(sealed)
	if err != nil || string(plain) != "token" {
		t.Fatalf("Open = %q, %v", plain, err)
	}
	sealed[len(sealed)-1] ^= 0xff
	if _, err := s.Open(sealed); err == nil {
		t.Fatal("tampered ciphertext must fail")
	}
	other, _ := NewSealer(bytes.Repeat([]byte{8}, 32))
	good, _ := s.Seal([]byte("token"))
	if _, err := other.Open(good); err == nil {
		t.Fatal("wrong key must fail")
	}
}

func TestFreshHomeUsesKeyringWhenAvailable(t *testing.T) {
	home := t.TempDir()
	kr, meta := newFakeKeyring(), memMeta{}
	k, err := LoadKey(home, kr, meta)
	if err != nil {
		t.Fatal(err)
	}
	if k.Backend != BackendKeyring {
		t.Fatalf("backend = %s", k.Backend)
	}
	if _, err := os.Stat(filepath.Join(home, "master.key")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("no key file expected with keyring backend")
	}
	again, err := LoadKey(home, kr, meta)
	if err != nil || !bytes.Equal(again.Bytes, k.Bytes) {
		t.Fatalf("reload mismatch: %v", err)
	}
}

func TestFreshHomeFallsBackToFile(t *testing.T) {
	home := t.TempDir()
	kr, meta := newFakeKeyring(), memMeta{}
	kr.unavailable = true
	k, err := LoadKey(home, kr, meta)
	if err != nil {
		t.Fatal(err)
	}
	if k.Backend != BackendFile || meta["key_backend"] != BackendFile {
		t.Fatalf("backend = %s, meta = %v", k.Backend, meta)
	}
	again, err := LoadKey(home, kr, meta)
	if err != nil || !bytes.Equal(again.Bytes, k.Bytes) {
		t.Fatalf("file key not reused: %v", err)
	}
}

func TestExistingKeyringHomeNeverCreatesSecondKey(t *testing.T) {
	home := t.TempDir()
	kr, meta := newFakeKeyring(), memMeta{}
	if _, err := LoadKey(home, kr, meta); err != nil {
		t.Fatal(err)
	}
	kr.unavailable = true // e.g. keyring times out at login
	if _, err := LoadKey(home, kr, meta); err == nil {
		t.Fatal("expected error when the recorded keyring backend is unavailable")
	}
	if _, err := os.Stat(filepath.Join(home, "master.key")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a file key must not be created for a home with sealed data")
	}
}

func TestMismatchedKeyFailsCheck(t *testing.T) {
	home := t.TempDir()
	kr, meta := newFakeKeyring(), memMeta{}
	if _, err := LoadKey(home, kr, meta); err != nil {
		t.Fatal(err)
	}
	for k := range kr.data {
		kr.data[k] = "00" + kr.data[k][2:]
	}
	if _, err := LoadKey(home, kr, meta); err == nil {
		t.Fatal("expected key check failure")
	}
}

func TestHomesDeriveDistinctKeyringUsers(t *testing.T) {
	if keyringUser("/a") == keyringUser("/b") {
		t.Fatal("different homes must use different keyring users")
	}
}
