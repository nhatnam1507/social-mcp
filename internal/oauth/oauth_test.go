package oauth

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"
)

func TestChallengeRFC7636Vector(t *testing.T) {
	const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	if got := ChallengeS256Base64URL(verifier); got != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Fatalf("base64url challenge = %s", got)
	}
	sum := sha256.Sum256([]byte(verifier))
	if got := ChallengeS256Hex(verifier); got != hex.EncodeToString(sum[:]) {
		t.Fatalf("hex challenge = %s", got)
	}
}

func TestVerifierShape(t *testing.T) {
	v := NewVerifier()
	if len(v) != 64 {
		t.Fatalf("len = %d", len(v))
	}
	for _, c := range v {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '.' || c == '_' || c == '~') {
			t.Fatalf("invalid verifier char %q", c)
		}
	}
}

func TestSessionStateIsSingleUse(t *testing.T) {
	s := NewSessions()
	sess := s.Begin("youtube", "http://127.0.0.1:1/oauth/callback", "")
	got, err := s.Take(sess.State)
	if err != nil || got.ID != sess.ID {
		t.Fatalf("Take = %v, %v", got, err)
	}
	if _, err := s.Take(sess.State); !errors.Is(err, ErrUnknownState) {
		t.Fatalf("second Take err = %v", err)
	}
}

func TestSessionExpiry(t *testing.T) {
	s := NewSessions()
	now := time.Now()
	s.now = func() time.Time { return now }
	sess := s.Begin("tiktok", "http://127.0.0.1:1/oauth/callback", "")
	now = now.Add(11 * time.Minute)
	if _, err := s.Take(sess.State); !errors.Is(err, ErrUnknownState) {
		t.Fatalf("expired Take err = %v", err)
	}
	got, ok := s.Get(sess.ID)
	if !ok || got.Status != StatusExpired {
		t.Fatalf("status = %v", got.Status)
	}
}

func TestSessionComplete(t *testing.T) {
	s := NewSessions()
	sess := s.Begin("youtube", "r", "ext-9")
	s.Complete(sess.ID, []string{"acc_1"}, nil)
	got, _ := s.Get(sess.ID)
	if got.Status != StatusSucceeded || got.AccountIDs[0] != "acc_1" || got.ReauthExternalID != "ext-9" {
		t.Fatalf("got %+v", got)
	}
	sess2 := s.Begin("youtube", "r", "")
	s.Complete(sess2.ID, nil, errors.New("boom"))
	got2, _ := s.Get(sess2.ID)
	if got2.Status != StatusFailed || got2.Error != "boom" {
		t.Fatalf("got %+v", got2)
	}
}
