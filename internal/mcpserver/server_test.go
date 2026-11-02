package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nhatnam1507/social-mcp/internal/service"
)

type fakeService struct {
	service.Service
	lastPlatform string
	lastInput    service.UploadInput
}

func (f *fakeService) ListAccounts(p string) ([]service.AccountView, error) {
	f.lastPlatform = p
	return []service.AccountView{{ID: "acc_1", Platform: "youtube", Health: "ok"}}, nil
}

func (f *fakeService) SubmitUpload(in service.UploadInput) (service.UploadSubmission, error) {
	f.lastInput = in
	if len(in.AccountIDs) == 0 {
		return service.UploadSubmission{}, service.Invalid("account_ids must list at least one account")
	}
	return service.UploadSubmission{Jobs: []service.JobRef{{JobID: "job_1", AccountID: in.AccountIDs[0]}}, Rejected: []service.Rejection{}}, nil
}

func (f *fakeService) GetUpload(id string) (service.JobView, error) {
	switch id {
	case "missing":
		return service.JobView{}, service.ErrNotFound
	case "leaky":
		return service.JobView{}, errors.New("upstream said access_token=SHOULDNOTLEAK")
	}
	return service.JobView{ID: id, Status: "succeeded", URL: "https://youtu.be/x"}, nil
}

func (f *fakeService) ListUploads(service.UploadFilter) ([]service.JobView, error) { return nil, nil }

func (f *fakeService) CancelUpload(id string) (service.JobView, error) {
	return service.JobView{ID: id, Status: "cancelled"}, nil
}

func connect(t *testing.T, svc service.Service) *mcp.ClientSession {
	t.Helper()
	ct, st := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := New(svc, "test").Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return res
}

func text(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func TestToolsAndAnnotations(t *testing.T) {
	cs := connect(t, &fakeService{})
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tools := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		tools[tool.Name] = tool
	}
	for _, name := range []string{"list_accounts", "upload_video", "get_upload_status", "list_uploads", "cancel_upload"} {
		if tools[name] == nil || tools[name].InputSchema == nil {
			t.Fatalf("tool %s missing or without schema", name)
		}
	}
	up := tools["upload_video"].Annotations
	if up.ReadOnlyHint || up.DestructiveHint == nil || !*up.DestructiveHint || up.OpenWorldHint == nil || !*up.OpenWorldHint {
		t.Fatalf("upload_video annotations %+v", up)
	}
	if !tools["list_accounts"].Annotations.ReadOnlyHint || !tools["get_upload_status"].Annotations.ReadOnlyHint {
		t.Fatal("read-only tools not annotated")
	}
	schema, _ := json.Marshal(tools["upload_video"].InputSchema)
	for _, field := range []string{"file_path", "account_ids", "privacy", "platform_options"} {
		if !strings.Contains(string(schema), field) {
			t.Fatalf("upload_video schema lacks %s: %s", field, schema)
		}
	}
}

func TestToolCalls(t *testing.T) {
	fake := &fakeService{}
	cs := connect(t, fake)

	res := call(t, cs, "list_accounts", map[string]any{"platform": "youtube"})
	if res.IsError || fake.lastPlatform != "youtube" || !strings.Contains(text(res), "acc_1") {
		t.Fatalf("list_accounts: %v %q", res.IsError, text(res))
	}

	res = call(t, cs, "upload_video", map[string]any{
		"file_path": "/videos/a.mp4", "account_ids": []string{"acc_1"}, "title": "Hi", "privacy": "public",
		"platform_options": map[string]any{"youtube": map[string]any{"made_for_kids": false}},
	})
	if res.IsError || fake.lastInput.Privacy != "public" || fake.lastInput.PlatformOptions["youtube"]["made_for_kids"] != false {
		t.Fatalf("upload_video: %q %+v", text(res), fake.lastInput)
	}

	if res = call(t, cs, "upload_video", map[string]any{"file_path": "/a.mp4", "account_ids": []string{}, "title": "x"}); !res.IsError {
		t.Fatal("validation error not reported as tool error")
	}
	if res = call(t, cs, "get_upload_status", map[string]any{"job_id": "missing"}); !res.IsError || !strings.Contains(text(res), "not found") {
		t.Fatalf("missing job: %q", text(res))
	}
	if res = call(t, cs, "get_upload_status", map[string]any{"job_id": "leaky"}); !res.IsError || strings.Contains(text(res), "SHOULDNOTLEAK") {
		t.Fatalf("leaky error: %q", text(res))
	}
	if res = call(t, cs, "list_uploads", map[string]any{}); res.IsError || !strings.Contains(text(res), `"jobs":[]`) {
		t.Fatalf("list_uploads: %q", text(res))
	}
	if res = call(t, cs, "cancel_upload", map[string]any{"job_id": "job_1"}); res.IsError || !strings.Contains(text(res), "cancelled") {
		t.Fatalf("cancel_upload: %q", text(res))
	}
}

type headerTransport struct {
	token string
	base  http.RoundTripper
}

func (h headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+h.token)
	return h.base.RoundTrip(r)
}

func TestStatelessHTTP(t *testing.T) {
	const token = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	server := New(&fakeService{}, "test")
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})
	srv := httptest.NewUnstartedServer(Protect(h, port, token))
	srv.Listener = ln
	srv.Start()
	defer srv.Close()
	endpoint := "http://127.0.0.1:" + strconv.Itoa(port) + "/mcp"

	for i := 0; i < 2; i++ {
		transport := &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: &http.Client{Transport: headerTransport{token: token, base: http.DefaultTransport}}}
		cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(context.Background(), transport, nil)
		if err != nil {
			t.Fatal(err)
		}
		res := call(t, cs, "list_accounts", nil)
		if res.IsError || !strings.Contains(text(res), "acc_1") {
			t.Fatalf("http list_accounts: %q", text(res))
		}
		cs.Close()
	}

	resp, err := http.Post(endpoint, "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no-token request status %d", resp.StatusCode)
	}
}
