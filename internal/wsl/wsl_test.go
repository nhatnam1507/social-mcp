package wsl

import "testing"

func TestIsWindowsPath(t *testing.T) {
	for p, want := range map[string]bool{
		`C:\Users\me\clip.mp4`:                    true,
		`d:/Videos/clip.mp4`:                      true,
		`\\wsl.localhost\Ubuntu\home\me\clip.mp4`: true,
		`/home/me/clip.mp4`:                       false,
		`clip.mp4`:                                false,
		`C:clip.mp4`:                              false,
	} {
		if got := IsWindowsPath(p); got != want {
			t.Errorf("IsWindowsPath(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestLinuxPath(t *testing.T) {
	if got := LinuxPath("/home/me/clip.mp4"); got != "/home/me/clip.mp4" {
		t.Fatalf("linux path changed to %q", got)
	}
	if !Detected() {
		if got := LinuxPath(`C:\clip.mp4`); got != `C:\clip.mp4` {
			t.Fatalf("outside WSL a Windows path must stay unchanged, got %q", got)
		}
		return
	}
	if got := LinuxPath(`C:\Users\clip.mp4`); got != "/mnt/c/Users/clip.mp4" {
		t.Fatalf("LinuxPath = %q", got)
	}
}
