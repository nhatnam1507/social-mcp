// Package service is the single API behind the control socket, the MCP tools and the
// stdio MCP mode.
package service

import (
	"errors"
	"fmt"
	"time"

	"github.com/nhatnam1507/social-mcp/internal/provider"
)

// Service is implemented by Local (in the daemon) and by control.Client (over the socket).
type Service interface {
	Status() (StatusView, error)
	ListAccounts(platform string) ([]AccountView, error)
	RemoveAccount(id string) error
	BeginAuth(platform, reauthAccountID string) (AuthStart, error)
	AuthSession(id string) (AuthSessionView, error)
	SubmitUpload(in UploadInput) (UploadSubmission, error)
	GetUpload(id string) (JobView, error)
	ListUploads(f UploadFilter) ([]JobView, error)
	CancelUpload(id string) (JobView, error)
}

// Health values.
const (
	HealthOK          = "ok"
	HealthExpiring    = "expiring"
	HealthNeedsReauth = "needs_reauth"
)

// UploadInput is an upload request from an agent or the CLI.
type UploadInput struct {
	FilePath        string                    `json:"file_path"`
	AccountIDs      []string                  `json:"account_ids"`
	Title           string                    `json:"title"`
	Description     string                    `json:"description,omitempty"`
	Tags            []string                  `json:"tags,omitempty"`
	Privacy         string                    `json:"privacy,omitempty"`
	PlatformOptions map[string]map[string]any `json:"platform_options,omitempty"`
}

// JobRef identifies a created job.
type JobRef struct {
	JobID     string `json:"job_id"`
	AccountID string `json:"account_id"`
	Platform  string `json:"platform"`
	Status    string `json:"status"`
}

// Rejection explains why an account was not given a job.
type Rejection struct {
	AccountID string `json:"account_id"`
	Reason    string `json:"reason"`
}

// UploadSubmission is the result of SubmitUpload.
type UploadSubmission struct {
	Jobs     []JobRef    `json:"jobs"`
	Rejected []Rejection `json:"rejected"`
}

// UploadFilter narrows ListUploads.
type UploadFilter struct {
	Status    string `json:"status,omitempty"`
	AccountID string `json:"account_id,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

// AccountView is a connected account as shown to agents and the CLI.
type AccountView struct {
	ID           string                `json:"account_id"`
	Platform     string                `json:"platform"`
	DisplayName  string                `json:"display_name"`
	ExternalID   string                `json:"external_id"`
	Health       string                `json:"health"`
	HealthDetail string                `json:"health_detail,omitempty"`
	Capabilities provider.Capabilities `json:"capabilities"`
	Options      []provider.OptionSpec `json:"options"`
}

// JobView is an upload job as shown to agents and the CLI.
type JobView struct {
	ID        string    `json:"job_id"`
	AccountID string    `json:"account_id"`
	Platform  string    `json:"platform"`
	FilePath  string    `json:"file_path"`
	Status    string    `json:"status"`
	Phase     string    `json:"phase"`
	BytesSent int64     `json:"bytes_sent"`
	FileSize  int64     `json:"file_size"`
	Percent   float64   `json:"percent"`
	Privacy   string    `json:"privacy,omitempty"`
	Outcome   string    `json:"outcome,omitempty"`
	RemoteID  string    `json:"remote_id,omitempty"`
	URL       string    `json:"url,omitempty"`
	Note      string    `json:"note,omitempty"`
	Error     string    `json:"error,omitempty"`
	Attempts  int       `json:"attempts"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// StatusView summarizes the daemon.
type StatusView struct {
	Version       string                    `json:"version"`
	PID           int                       `json:"pid"`
	StartedAt     time.Time                 `json:"started_at"`
	ListenURL     string                    `json:"listen_url"`
	KeyBackend    string                    `json:"key_backend"`
	Platforms     []string                  `json:"platforms"`
	Accounts      map[string]map[string]int `json:"accounts"`
	ActiveUploads int                       `json:"active_uploads"`
}

// AuthStart is returned when a login begins.
type AuthStart struct {
	SessionID string `json:"session_id"`
	AuthURL   string `json:"auth_url"`
}

// AuthSessionView reports a login's progress.
type AuthSessionView struct {
	ID       string        `json:"session_id"`
	Platform string        `json:"platform"`
	State    string        `json:"state"`
	Accounts []AccountView `json:"accounts,omitempty"`
	Error    string        `json:"error,omitempty"`
}

// ErrNotFound is returned for unknown IDs.
var ErrNotFound = errors.New("not found")

// ValidationError is a request the caller must fix.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

// Invalid returns a ValidationError.
func Invalid(format string, args ...any) error {
	return &ValidationError{Msg: fmt.Sprintf(format, args...)}
}

// IsValidation reports whether err is a ValidationError.
func IsValidation(err error) bool {
	var v *ValidationError
	return errors.As(err, &v)
}
