package refresher_test

import (
	"context"
	"testing"
	"time"

	"github.com/nhatnam1507/social-mcp/internal/enginetest"
	"github.com/nhatnam1507/social-mcp/internal/provider"
	"github.com/nhatnam1507/social-mcp/internal/refresher"
	"github.com/nhatnam1507/social-mcp/internal/store"
)

func TestRunOnce(t *testing.T) {
	env := enginetest.New(t, "youtube", "instagram", "tiktok")
	soon := provider.Token{AccessToken: "a", RefreshToken: "r", Expiry: time.Now().Add(5 * time.Minute)}
	later := provider.Token{AccessToken: "a", RefreshToken: "r", Expiry: time.Now().Add(2 * time.Hour)}
	ytCred, ytAcc := env.AddAccount(enginetest.AccountSpec{Platform: "youtube", ExternalID: "ch1", Refreshable: true, Token: soon})
	env.AddAccount(enginetest.AccountSpec{Platform: "tiktok", ExternalID: "u1", Refreshable: true, Token: later})
	igCred, _ := env.AddAccount(enginetest.AccountSpec{Platform: "instagram", ExternalID: "ig1",
		Token: provider.Token{AccessToken: "user", Expiry: time.Now().Add(-time.Hour)}})

	r := &refresher.Refresher{Store: env.Store, Tokens: env.Tokens, Log: env.Log}
	r.RunOnce(context.Background())

	if env.Fake("youtube").Refreshes() != 1 || env.Fake("tiktok").Refreshes() != 0 {
		t.Fatalf("refreshes yt=%d tt=%d", env.Fake("youtube").Refreshes(), env.Fake("tiktok").Refreshes())
	}
	if l, _ := env.Tokens.ForAccount(ytAcc); l.Token.AccessToken != "a+r" {
		t.Fatalf("youtube token not refreshed: %+v", l.Token)
	}
	if c, _ := env.Store.GetCredential(ytCred.ID); c.Status != store.StatusOK {
		t.Fatalf("youtube status %s", c.Status)
	}
	if c, _ := env.Store.GetCredential(igCred.ID); c.Status != store.StatusNeedsReauth {
		t.Fatalf("expired non-refreshable credential status %s", c.Status)
	}
}
