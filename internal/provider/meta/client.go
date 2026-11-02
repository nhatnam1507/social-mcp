// Package meta is the Graph API client shared by the Facebook and Instagram providers.
package meta

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nhatnam1507/social-mcp/internal/provider"
)

// Config holds the Meta app settings.
type Config struct {
	AppID         string
	AppSecret     string
	LoginConfigID string
	GraphVersion  string
	AppMode       string // "development" or "live"
}

// Client calls the Graph API.
type Client struct {
	Config
	GraphBase   string
	VideoBase   string
	RuploadBase string
	DialogBase  string
	HTTP        *http.Client
}

// NewClient returns a client for the configured Graph API version.
func NewClient(cfg Config) *Client {
	v := cfg.GraphVersion
	return &Client{
		Config:      cfg,
		GraphBase:   "https://graph.facebook.com/" + v,
		VideoBase:   "https://graph-video.facebook.com/" + v,
		RuploadBase: "https://rupload.facebook.com/ig-api-upload/" + v,
		DialogBase:  "https://www.facebook.com/" + v + "/dialog/oauth",
		HTTP:        &http.Client{Timeout: 10 * time.Minute},
	}
}

// Restrictions returns restrictions every Meta account shares.
func (c *Client) Restrictions() []string {
	if c.AppMode == "live" {
		return []string{}
	}
	return []string{provider.RestrictionVisibleToAppRolesOnly}
}

// GraphError is a Graph API error.
type GraphError struct {
	Status    int
	Message   string `json:"message"`
	Type      string `json:"type"`
	Code      int    `json:"code"`
	Subcode   int    `json:"error_subcode"`
	Transient bool   `json:"is_transient"`
}

func (e *GraphError) Error() string {
	return fmt.Sprintf("Meta Graph API error %d/%d (HTTP %d): %s", e.Code, e.Subcode, e.Status, e.Message)
}

// classify maps a Graph error to the provider error taxonomy.
func classify(resp *http.Response, body []byte) error {
	var env struct {
		Error *GraphError `json:"error"`
	}
	json.Unmarshal(body, &env)
	if env.Error == nil {
		if resp.StatusCode >= 500 {
			return fmt.Errorf("Meta returned %s", resp.Status)
		}
		return provider.Permanent("Meta returned %s", resp.Status)
	}
	ge := env.Error
	ge.Status = resp.StatusCode
	switch {
	case ge.Code == 190:
		// Meta tokens cannot be refreshed: an invalid or expired token needs a new login.
		return fmt.Errorf("%w: %v", provider.ErrReauthRequired, ge)
	case ge.Code == 4 || ge.Code == 17 || ge.Code == 32 || ge.Code == 341 || ge.Code == 613 ||
		(ge.Code >= 80000 && ge.Code <= 80014) || resp.StatusCode == http.StatusTooManyRequests:
		return &provider.RateLimitError{RetryAfter: provider.RetryAfter(resp.Header, 5*time.Minute), Msg: ge.Message}
	case ge.Transient || resp.StatusCode >= 500 || ge.Code == 1 || ge.Code == 2:
		return ge
	case ge.Code == 10 || (ge.Code >= 200 && ge.Code < 300):
		return &provider.PermanentError{Msg: "Meta denied permission: " + ge.Message + " (check the app's permissions and mode)", Err: ge}
	}
	return &provider.PermanentError{Msg: ge.Error(), Err: ge}
}

// IsNotFound reports whether err is a Graph "object does not exist" error.
func IsNotFound(err error) bool {
	var ge *GraphError
	return errors.As(err, &ge) && ge.Code == 100
}

// Get calls GET base+path with params and decodes the JSON response into out.
func (c *Client) Get(ctx context.Context, base, path string, params url.Values, token string, out any) error {
	u := base + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	return c.getURL(ctx, u, token, out)
}

func (c *Client) getURL(ctx context.Context, u, token string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	return c.do(req, token, out)
}

// PostForm calls POST base+path with a form body.
func (c *Client) PostForm(ctx context.Context, base, path string, form url.Values, token string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.do(req, token, out)
}

// PostMultipart calls POST base+path with form fields and one file part of size bytes,
// streaming the file instead of buffering it.
func (c *Client) PostMultipart(ctx context.Context, base, path string, fields map[string]string, fileField string, file io.Reader, size int64, token string, out any) error {
	var head bytes.Buffer
	mw := multipart.NewWriter(&head)
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			return err
		}
	}
	if _, err := mw.CreateFormFile(fileField, "chunk"); err != nil {
		return err
	}
	tail := "\r\n--" + mw.Boundary() + "--\r\n"
	body := io.MultiReader(bytes.NewReader(head.Bytes()), io.LimitReader(file, size), strings.NewReader(tail))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, body)
	if err != nil {
		return err
	}
	req.ContentLength = int64(head.Len()) + size + int64(len(tail))
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return c.do(req, token, out)
}

// Do sends a prepared request with the token and decodes the response.
func (c *Client) Do(req *http.Request, token string, out any) error { return c.do(req, token, out) }

func (c *Client) do(req *http.Request, token string, out any) error {
	if token != "" {
		req.Header.Set("Authorization", "OAuth "+token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != http.StatusOK {
		return classify(resp, body)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode Meta response: %w", err)
	}
	return nil
}

// GetAll follows Graph paging for an edge returning {"data": [...]}, staying on the Graph host.
func GetAll[T any](ctx context.Context, c *Client, path string, params url.Values, token string) ([]T, error) {
	var all []T
	u := c.GraphBase + path + "?" + params.Encode()
	graphHost := hostOf(c.GraphBase)
	for page := 0; u != "" && page < 50; page++ {
		var resp struct {
			Data   []T `json:"data"`
			Paging struct {
				Next string `json:"next"`
			} `json:"paging"`
		}
		if err := c.getURL(ctx, u, token, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.Data...)
		u = ""
		if resp.Paging.Next != "" && hostOf(resp.Paging.Next) == graphHost {
			u = resp.Paging.Next
		}
	}
	return all, nil
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}
