package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/nhatnam1507/social-mcp/internal/service"
)

// ErrDaemonNotRunning means nothing answers on the control socket.
var ErrDaemonNotRunning = errors.New("daemon is not running (start it with `social-mcp daemon start`)")

// Client calls the control API over the socket and implements service.Service.
type Client struct {
	socket string
	http   *http.Client
}

var _ service.Service = (*Client)(nil)

// NewClient returns a client for the socket at path.
func NewClient(socket string) *Client {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}
	return &Client{socket: socket, http: &http.Client{Transport: tr, Timeout: 60 * time.Second}}
}

// Ping reports whether the daemon answers.
func (c *Client) Ping() bool {
	_, err := c.Status()
	return err == nil
}

// Shutdown asks the daemon to exit.
func (c *Client) Shutdown() error {
	var out struct{}
	return c.do(http.MethodPost, "/v1/shutdown", nil, &out)
}

func (c *Client) Status() (service.StatusView, error) {
	var v service.StatusView
	return v, c.do(http.MethodGet, "/v1/status", nil, &v)
}

func (c *Client) ListAccounts(platform string) ([]service.AccountView, error) {
	var v []service.AccountView
	return v, c.do(http.MethodGet, "/v1/accounts?platform="+url.QueryEscape(platform), nil, &v)
}

func (c *Client) RemoveAccount(id string) error {
	var v struct{}
	return c.do(http.MethodDelete, "/v1/accounts/"+url.PathEscape(id), nil, &v)
}

func (c *Client) BeginAuth(platform, reauthAccountID string) (service.AuthStart, error) {
	var v service.AuthStart
	return v, c.do(http.MethodPost, "/v1/auth", authRequest{Platform: platform, ReauthAccountID: reauthAccountID}, &v)
}

func (c *Client) AuthSession(id string) (service.AuthSessionView, error) {
	var v service.AuthSessionView
	return v, c.do(http.MethodGet, "/v1/auth/"+url.PathEscape(id), nil, &v)
}

func (c *Client) SubmitUpload(in service.UploadInput) (service.UploadSubmission, error) {
	var v service.UploadSubmission
	return v, c.do(http.MethodPost, "/v1/uploads", in, &v)
}

func (c *Client) GetUpload(id string) (service.JobView, error) {
	var v service.JobView
	return v, c.do(http.MethodGet, "/v1/uploads/"+url.PathEscape(id), nil, &v)
}

func (c *Client) ListUploads(f service.UploadFilter) ([]service.JobView, error) {
	q := url.Values{}
	if f.Status != "" {
		q.Set("status", f.Status)
	}
	if f.AccountID != "" {
		q.Set("account_id", f.AccountID)
	}
	if f.Limit > 0 {
		q.Set("limit", strconv.Itoa(f.Limit))
	}
	var v []service.JobView
	return v, c.do(http.MethodGet, "/v1/uploads?"+q.Encode(), nil, &v)
}

func (c *Client) CancelUpload(id string) (service.JobView, error) {
	var v service.JobView
	return v, c.do(http.MethodPost, "/v1/uploads/"+url.PathEscape(id)+"/cancel", nil, &v)
}

func (c *Client) do(method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, "http://social-mcp"+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		var opErr *net.OpError
		if errors.As(err, &opErr) && opErr.Op == "dial" {
			return ErrDaemonNotRunning
		}
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		var e errorBody
		if json.Unmarshal(data, &e) != nil {
			return fmt.Errorf("daemon returned %s", resp.Status)
		}
		switch e.Kind {
		case "not_found":
			return service.ErrNotFound
		case "validation":
			return &service.ValidationError{Msg: e.Error}
		}
		return errors.New(e.Error)
	}
	if resp.StatusCode == http.StatusAccepted || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}
