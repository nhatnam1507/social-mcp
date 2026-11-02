package mcptoken

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadOrCreate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp-token")
	tok, err := LoadOrCreate(path)
	if err != nil || len(tok) != 64 {
		t.Fatalf("created %q, %v", tok, err)
	}
	if fi, _ := os.Stat(path); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", fi.Mode().Perm())
	}
	again, err := LoadOrCreate(path)
	if err != nil || again != tok {
		t.Fatalf("reload = %q, %v", again, err)
	}
	rotated, err := Rotate(path)
	if err != nil || rotated == tok {
		t.Fatalf("rotate = %q, %v", rotated, err)
	}
}

func TestLoadRejectsBadFiles(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{"empty": "", "short": "abcd", "nonhex": string(make([]byte, 64))} {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte(content), 0o600)
		if _, err := Load(p); err == nil {
			t.Fatalf("%s accepted", name)
		}
		if _, err := LoadOrCreate(p); err == nil {
			t.Fatalf("LoadOrCreate replaced invalid %s silently", name)
		}
	}
	if _, err := Load(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing accepted")
	}
}
