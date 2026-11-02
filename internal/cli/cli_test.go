package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nhatnam1507/social-mcp/internal/control"
	"github.com/nhatnam1507/social-mcp/internal/paths"
	"github.com/nhatnam1507/social-mcp/internal/provider"
	"github.com/nhatnam1507/social-mcp/internal/service"
)

// stubService answers CLI calls without a daemon.
type stubService struct {
	service.Service
	accounts []service.AccountView
	removed  string
	session  service.AuthSessionView
}

func (s *stubService) Status() (service.StatusView, error) {
	return service.StatusView{}, control.ErrDaemonNotRunning
}
func (s *stubService) ListAccounts(string) ([]service.AccountView, error) { return s.accounts, nil }
func (s *stubService) RemoveAccount(id string) error                      { s.removed = id; return nil }
func (s *stubService) BeginAuth(platform, reauth string) (service.AuthStart, error) {
	return service.AuthStart{SessionID: "auth_1", AuthURL: "https://evil.example/not-opened"}, nil
}
func (s *stubService) AuthSession(string) (service.AuthSessionView, error) { return s.session, nil }

func run(t *testing.T, svc service.Service, args ...string) (string, error) {
	t.Helper()
	h := t.TempDir()
	resolveHome = func() (string, error) { return h, nil }
	dialService = func(string) service.Service { return svc }
	var out bytes.Buffer
	root := NewRoot("test", &out, &out)
	root.SetArgs(args)
	root.SetIn(strings.NewReader("y\n"))
	err := root.Execute()
	return out.String(), err
}

func TestConfigInitRefusesOverwrite(t *testing.T) {
	h := t.TempDir()
	resolveHome = func() (string, error) { return h, nil }
	var out bytes.Buffer
	root := NewRoot("test", &out, &out)
	root.SetArgs([]string{"config", "init"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.Config(h)); err != nil {
		t.Fatal(err)
	}
	root = NewRoot("test", &out, &out)
	root.SetArgs([]string{"config", "init"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second init: %v", err)
	}
	root = NewRoot("test", &out, &out)
	root.SetArgs([]string{"config", "init", "--force"})
	if err := root.Execute(); err != nil {
		t.Fatalf("force: %v", err)
	}
}

func TestStatusExitCodeWhenStopped(t *testing.T) {
	out, err := run(t, &stubService{}, "daemon", "status")
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != 3 || !strings.Contains(out, "stopped") {
		t.Fatalf("status: %q %v", out, err)
	}
}

func TestAccountListJSON(t *testing.T) {
	svc := &stubService{accounts: []service.AccountView{{ID: "acc_1", Platform: "tiktok", DisplayName: "Me", Health: "ok",
		Capabilities: provider.Capabilities{Restrictions: []string{"draft_only"}}}}}
	out, err := run(t, svc, "account", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var got []service.AccountView
	if err := json.Unmarshal([]byte(out), &got); err != nil || got[0].ID != "acc_1" {
		t.Fatalf("json output %q: %v", out, err)
	}
	out, _ = run(t, svc, "account", "list")
	if !strings.Contains(out, "draft_only") {
		t.Fatalf("table output %q", out)
	}
}

func TestAccountRemoveConfirms(t *testing.T) {
	svc := &stubService{}
	if _, err := run(t, svc, "account", "remove", "acc_9"); err != nil || svc.removed != "acc_9" {
		t.Fatalf("remove: %v removed=%q", err, svc.removed)
	}
}

func TestAccountAddPrintsURLAndWaits(t *testing.T) {
	authPoll, authTimeout = time.Millisecond, time.Second
	svc := &stubService{session: service.AuthSessionView{State: "succeeded", Accounts: []service.AccountView{{ID: "acc_1", Platform: "youtube", DisplayName: "Chan"}}}}
	out, err := run(t, svc, "account", "add", "youtube")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Open this URL") || !strings.Contains(out, "Chan") {
		t.Fatalf("output %q", out)
	}
	svc.session = service.AuthSessionView{State: "failed", Error: "denied"}
	if _, err := run(t, svc, "account", "add", "youtube"); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("failed login: %v", err)
	}
}

func TestConfigMCPPrintsSnippet(t *testing.T) {
	out, err := run(t, &stubService{}, "config", "mcp")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "claude mcp add --transport http social-mcp http://127.0.0.1:8765/mcp") || !strings.Contains(out, `"args": ["mcp"]`) {
		t.Fatalf("output %q", out)
	}
}
