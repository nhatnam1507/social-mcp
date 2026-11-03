// Package videofile validates local video files before and during upload.
package videofile

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/nhatnam1507/social-mcp/internal/provider"
)

// Info describes a validated file.
type Info struct {
	Path        string // resolved absolute path
	Size        int64
	ModTime     time.Time
	Fingerprint string
	Container   string
}

const fingerprintChunk = 1 << 20

var (
	mp4Brands   = []string{"isom", "iso2", "iso3", "iso4", "iso5", "iso6", "mp41", "mp42", "avc1", "M4V ", "M4VH", "M4VP", "dash", "3gp4", "3gp5", "3gp6"}
	imageBrands = []string{"heic", "heix", "hevc", "hevx", "heim", "heis", "mif1", "msf1", "avif", "avis"}
)

// Check validates path: absolute, inside allowedDirs when any are configured, a non-empty
// regular file, and a recognized video container.
func Check(path string, allowedDirs []string) (Info, error) {
	if !filepath.IsAbs(path) {
		return Info{}, fmt.Errorf("file path must be absolute: %q", path)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return Info{}, fmt.Errorf("cannot access %s: %w", path, err)
	}
	if err := checkAllowed(resolved, allowedDirs); err != nil {
		return Info{}, err
	}
	if err := requireRegular(resolved); err != nil {
		return Info{}, err
	}
	f, err := os.Open(resolved)
	if err != nil {
		return Info{}, err
	}
	defer f.Close()
	return inspect(f, resolved)
}

// Open re-validates info and returns a handle to read from. It fails if the file changed
// since it was checked.
func Open(info Info, allowedDirs []string) (*os.File, error) {
	resolved, err := filepath.EvalSymlinks(info.Path)
	if err != nil {
		return nil, provider.Permanent("video file is no longer accessible: %v", err)
	}
	if err := checkAllowed(resolved, allowedDirs); err != nil {
		return nil, provider.Permanent("%v", err)
	}
	if err := requireRegular(resolved); err != nil {
		return nil, provider.Permanent("%v", err)
	}
	f, err := os.Open(resolved)
	if err != nil {
		return nil, provider.Permanent("cannot open video file: %v", err)
	}
	cur, err := inspect(f, resolved)
	if err != nil {
		f.Close()
		return nil, provider.Permanent("%v", err)
	}
	if cur.Size != info.Size || !cur.ModTime.Equal(info.ModTime) || cur.Fingerprint != info.Fingerprint {
		f.Close()
		return nil, provider.Permanent("video file changed since it was submitted; submit it again")
	}
	return f, nil
}

func inspect(f *os.File, resolved string) (Info, error) {
	fi, err := f.Stat()
	if err != nil {
		return Info{}, err
	}
	if !fi.Mode().IsRegular() {
		return Info{}, fmt.Errorf("%s is not a regular file", resolved)
	}
	if fi.Size() == 0 {
		return Info{}, fmt.Errorf("%s is empty", resolved)
	}
	head := make([]byte, 64)
	n, err := f.ReadAt(head, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return Info{}, err
	}
	container, err := sniff(head[:n])
	if err != nil {
		return Info{}, fmt.Errorf("%s: %w", resolved, err)
	}
	fp, err := fingerprint(f, fi.Size())
	if err != nil {
		return Info{}, err
	}
	return Info{Path: resolved, Size: fi.Size(), ModTime: fi.ModTime(), Fingerprint: fp, Container: container}, nil
}

func sniff(head []byte) (string, error) {
	if len(head) >= 12 && string(head[4:8]) == "ftyp" {
		size := int(binary.BigEndian.Uint32(head[:4]))
		if size < 16 || size > len(head) {
			size = len(head)
		}
		brands := []string{string(head[8:12])}
		for i := 16; i+4 <= size; i += 4 {
			brands = append(brands, string(head[i:i+4]))
		}
		for _, b := range brands {
			if contains(imageBrands, b) {
				return "", errors.New("file is an image, not a video")
			}
		}
		if brands[0] == "qt  " {
			return provider.FormatMOV, nil
		}
		for _, b := range brands {
			if contains(mp4Brands, b) {
				return provider.FormatMP4, nil
			}
		}
		return "", fmt.Errorf("unsupported ISO-BMFF brand %q", brands[0])
	}
	if bytes.HasPrefix(head, []byte{0x1A, 0x45, 0xDF, 0xA3}) {
		switch {
		case bytes.Contains(head, []byte("webm")):
			return provider.FormatWebM, nil
		case bytes.Contains(head, []byte("matroska")):
			return provider.FormatMKV, nil
		}
		return "", errors.New("unsupported EBML document type")
	}
	return "", errors.New("not a recognized video file (expected MP4, MOV, WebM or MKV)")
}

// fingerprint hashes the size plus the first and last MiB.
func fingerprint(f io.ReaderAt, size int64) (string, error) {
	h := sha256.New()
	fmt.Fprintf(h, "%d:", size)
	buf := make([]byte, min(size, fingerprintChunk))
	if _, err := f.ReadAt(buf, 0); err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	h.Write(buf)
	if size > fingerprintChunk {
		if _, err := f.ReadAt(buf, size-int64(len(buf))); err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		h.Write(buf)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// requireRegular rejects FIFOs, devices and directories before opening, since opening a
// FIFO blocks until a writer appears.
func requireRegular(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	return nil
}

func checkAllowed(resolved string, allowedDirs []string) error {
	if len(allowedDirs) == 0 {
		return nil
	}
	for _, d := range allowedDirs {
		dir, err := os.Stat(d)
		if err != nil || !dir.IsDir() {
			continue
		}
		if within(resolved, dir) {
			return nil
		}
	}
	return fmt.Errorf("%s is outside uploads.allowed_dirs", resolved)
}

// within reports whether path lies below dir. It compares directory identity rather than
// spelling, so it is correct on case-insensitive filesystems (Windows, default macOS).
func within(path string, dir os.FileInfo) bool {
	for p := filepath.Dir(path); ; {
		if fi, err := os.Stat(p); err == nil && os.SameFile(fi, dir) {
			return true
		}
		parent := filepath.Dir(p)
		if parent == p {
			return false
		}
		p = parent
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
