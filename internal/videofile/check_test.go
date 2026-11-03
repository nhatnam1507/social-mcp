package videofile

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/nhatnam1507/social-mcp/internal/provider"
)

// ftyp builds an ISO-BMFF header with the given major and compatible brands.
func ftyp(major string, compat ...string) []byte {
	size := 16 + 4*len(compat)
	b := make([]byte, 0, size+64)
	b = binary.BigEndian.AppendUint32(b, uint32(size))
	b = append(b, "ftyp"...)
	b = append(b, major...)
	b = append(b, 0, 0, 0, 0)
	for _, c := range compat {
		b = append(b, c...)
	}
	return append(b, make([]byte, 64)...)
}

// ebml builds a minimal EBML header with the given DocType.
func ebml(docType string) []byte {
	b := []byte{0x1A, 0x45, 0xDF, 0xA3, 0x80 | byte(3+len(docType))}
	b = append(b, 0x42, 0x82, 0x80|byte(len(docType)))
	b = append(b, docType...)
	return append(b, make([]byte, 64)...)
}

func write(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestContainerDetection(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		data []byte
		want string
		ok   bool
	}{
		{"mp4", ftyp("isom", "isom", "mp41"), provider.FormatMP4, true},
		{"m4v", ftyp("M4V ", "M4V "), provider.FormatMP4, true},
		{"mov", ftyp("qt  ", "qt  "), provider.FormatMOV, true},
		{"webm", ebml("webm"), provider.FormatWebM, true},
		{"mkv", ebml("matroska"), provider.FormatMKV, true},
		{"heic", ftyp("heic", "mif1", "heic"), "", false},
		{"avif", ftyp("avif", "avif", "mif1"), "", false},
		{"text", []byte("hello world, definitely not a video file at all...................................."), "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info, err := Check(write(t, dir, tc.name, tc.data), nil)
			if tc.ok != (err == nil) {
				t.Fatalf("err = %v, want ok=%v", err, tc.ok)
			}
			if tc.ok && info.Container != tc.want {
				t.Fatalf("container = %s, want %s", info.Container, tc.want)
			}
		})
	}
}

func TestPathRules(t *testing.T) {
	base := t.TempDir()
	allowed := filepath.Join(base, "videos")
	sibling := filepath.Join(base, "videos2")
	for _, d := range []string{allowed, sibling} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	inside := write(t, allowed, "a.mp4", ftyp("isom"))
	outside := write(t, sibling, "b.mp4", ftyp("isom"))

	if _, err := Check(inside, []string{allowed}); err != nil {
		t.Fatalf("inside rejected: %v", err)
	}
	if _, err := Check(outside, []string{allowed}); err == nil {
		t.Fatal("sibling-prefix dir accepted")
	}
	if _, err := Check(filepath.Join(allowed, "..", "videos2", "b.mp4"), []string{allowed}); err == nil {
		t.Fatal("traversal accepted")
	}
	if _, err := Check(outside, nil); err != nil {
		t.Fatalf("no restriction should accept any path: %v", err)
	}
	if _, err := Check("relative.mp4", nil); err == nil {
		t.Fatal("relative path accepted")
	}
	if _, err := Check(allowed, nil); err == nil {
		t.Fatal("directory accepted")
	}
	empty := write(t, allowed, "empty.mp4", nil)
	if _, err := Check(empty, nil); err == nil {
		t.Fatal("empty file accepted")
	}
}

func TestSymlinkEscapeRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	base := t.TempDir()
	allowed := filepath.Join(base, "videos")
	os.Mkdir(allowed, 0o700)
	secret := write(t, base, "secret.mp4", ftyp("isom"))
	link := filepath.Join(allowed, "link.mp4")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(link, []string{allowed}); err == nil {
		t.Fatal("symlink escaping the allowed dir accepted")
	}
}

func TestOpenDetectsChangedFile(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "v.mp4", ftyp("isom"))
	info, err := Check(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	f, err := Open(info, nil)
	if err != nil {
		t.Fatalf("unchanged file rejected: %v", err)
	}
	f.Close()

	data := ftyp("isom")
	data[len(data)-1] = 1 // same size, different content
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(p, info.ModTime, info.ModTime) // keep mtime so only the fingerprint differs
	if _, err := Open(info, nil); err == nil {
		t.Fatal("changed content accepted")
	}

	os.WriteFile(p, append(ftyp("isom"), 1, 2, 3), 0o600)
	later := time.Now().Add(time.Hour)
	os.Chtimes(p, later, later)
	if _, err := Open(info, nil); err == nil {
		t.Fatal("changed size accepted")
	}
}
