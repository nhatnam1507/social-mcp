package tokens_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nhatnam1507/social-mcp/internal/enginetest"
	"github.com/nhatnam1507/social-mcp/internal/provider"
	"github.com/nhatnam1507/social-mcp/internal/store"
)

func TestConcurrentRefreshCallsProviderOnce(t *testing.T) {
	env := enginetest.New(t, "tiktok")
	fake := env.Fake("tiktok")
	release := make(chan struct{})
	fake.RefreshFunc = func(tok provider.Token) (provider.Token, error) {
		<-release
		return provider.Token{AccessToken: "new", RefreshToken: "rotated", Expiry: time.Now().Add(2 * time.Hour)}, nil
	}
	_, acc := env.AddAccount(enginetest.AccountSpec{Platform: "tiktok", ExternalID: "u1", Refreshable: true})
	lease, err := env.Tokens.ForAccount(acc)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make([]string, 4)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			l, err := env.Tokens.Refresh(context.Background(), lease)
			if err != nil {
				t.Error(err)
				return
			}
			results[i] = l.Token.AccessToken
		}(i)
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	if fake.Refreshes() != 1 {
		t.Fatalf("provider refreshed %d times", fake.Refreshes())
	}
	for _, r := range results {
		if r != "new" {
			t.Fatalf("results = %v", results)
		}
	}
	stored, _ := env.Tokens.ForAccount(acc)
	if stored.Token.RefreshToken != "rotated" {
		t.Fatalf("rotated refresh token not persisted: %+v", stored.Token)
	}
}

func TestStaleLeaseDoesNotReuseRotatedToken(t *testing.T) {
	env := enginetest.New(t, "tiktok")
	fake := env.Fake("tiktok")
	fake.RefreshFunc = func(tok provider.Token) (provider.Token, error) {
		if tok.RefreshToken != "refresh" {
			return provider.Token{}, provider.ErrReauthRequired // old refresh token was rotated away
		}
		return provider.Token{AccessToken: "t2", RefreshToken: "r2"}, nil
	}
	_, acc := env.AddAccount(enginetest.AccountSpec{Platform: "tiktok", ExternalID: "u1", Refreshable: true})
	stale, _ := env.Tokens.ForAccount(acc)
	if _, err := env.Tokens.Refresh(context.Background(), stale); err != nil {
		t.Fatal(err)
	}
	// A second caller still holding the pre-rotation lease gets the stored token, no provider call.
	l, err := env.Tokens.Refresh(context.Background(), stale)
	if err != nil || l.Token.AccessToken != "t2" || fake.Refreshes() != 1 {
		t.Fatalf("stale refresh: %+v %v calls=%d", l.Token, err, fake.Refreshes())
	}
}

func TestRefreshReauthMarksCredential(t *testing.T) {
	env := enginetest.New(t, "youtube")
	env.Fake("youtube").RefreshFunc = func(provider.Token) (provider.Token, error) { return provider.Token{}, provider.ErrReauthRequired }
	cred, acc := env.AddAccount(enginetest.AccountSpec{Platform: "youtube", ExternalID: "ch1", Refreshable: true})
	lease, _ := env.Tokens.ForAccount(acc)
	if _, err := env.Tokens.Refresh(context.Background(), lease); !errors.Is(err, provider.ErrReauthRequired) {
		t.Fatalf("err = %v", err)
	}
	c, _ := env.Store.GetCredential(cred.ID)
	if c.Status != store.StatusNeedsReauth {
		t.Fatalf("status = %s", c.Status)
	}
}

func TestConcurrentReloginWins(t *testing.T) {
	env := enginetest.New(t, "youtube")
	cred, acc := env.AddAccount(enginetest.AccountSpec{Platform: "youtube", ExternalID: "ch1", Refreshable: true})
	fake := env.Fake("youtube")
	fake.RefreshFunc = func(provider.Token) (provider.Token, error) {
		// A re-login lands while the refresh call is in flight.
		sealed, _ := env.Tokens.Seal(provider.Token{AccessToken: "relogin", RefreshToken: "relogin-r"})
		if _, _, err := env.Store.UpsertLogin(store.Credential{Platform: "youtube", Subject: cred.Subject, SealedToken: sealed, Refreshable: true},
			[]store.Account{{Platform: "youtube", ExternalID: "ch1", DisplayName: "x"}}); err != nil {
			t.Error(err)
		}
		return provider.Token{AccessToken: "refreshed"}, nil
	}
	lease, _ := env.Tokens.ForAccount(acc)
	l, err := env.Tokens.Refresh(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := env.Tokens.ForAccount(acc)
	if stored.Token.AccessToken != "relogin" || l.Token.AccessToken != "relogin" {
		t.Fatalf("re-login token overwritten: stored=%s returned=%s", stored.Token.AccessToken, l.Token.AccessToken)
	}
}
