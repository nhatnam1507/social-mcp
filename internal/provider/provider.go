// Package provider defines the contract every platform integration implements.
package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// Publish modes.
const (
	PublishDirect = "direct"
	PublishDraft  = "draft"
)

// Privacy levels.
const (
	PrivacyPrivate  = "private"
	PrivacyUnlisted = "unlisted"
	PrivacyPublic   = "public"
)

// Container formats detected by the file guard.
const (
	FormatMP4  = "mp4"
	FormatMOV  = "mov"
	FormatWebM = "webm"
	FormatMKV  = "mkv"
)

// Outcomes reported by a finished upload.
const (
	OutcomePublished    = "published"
	OutcomeUnpublished  = "unpublished"
	OutcomeSentToDrafts = "sent_to_drafts"
)

// Upload phases. Once a job reports PhaseCommitting, a provider must reconcile with the
// remote instead of repeating any step that could publish.
const (
	PhasePending      = "pending"
	PhaseTransferring = "transferring"
	PhaseCommitting   = "committing"
	PhaseProcessing   = "processing"
	PhaseDone         = "done"
)

// Restrictions surfaced to agents.
const (
	RestrictionDraftOnly             = "draft_only"
	RestrictionPrivacySetInApp       = "privacy_set_in_app"
	RestrictionNoPrivateOption       = "no_private_option"
	RestrictionVisibleToAppRolesOnly = "visible_to_app_roles_only"
)

// Token is an OAuth token set.
type Token struct {
	AccessToken   string            `json:"access_token"`
	RefreshToken  string            `json:"refresh_token,omitempty"`
	Expiry        time.Time         `json:"expiry,omitzero"`
	RefreshExpiry time.Time         `json:"refresh_expiry,omitzero"`
	Extra         map[string]string `json:"extra,omitempty"`
}

// Identity is one publish target reachable with a login.
type Identity struct {
	ExternalID   string
	DisplayName  string
	AccountToken *Token // set when the target has its own token (e.g. a Facebook Page token)
	Meta         map[string]string
}

// Capabilities describe what an account can do.
type Capabilities struct {
	PublishMode    string   `json:"publish_mode"`
	PrivacyLevels  []string `json:"privacy_levels"`
	Restrictions   []string `json:"restrictions"`
	MaxFileBytes   int64    `json:"max_file_bytes,omitempty"`
	MaxDurationSec int      `json:"max_duration_sec,omitempty"`
	Formats        []string `json:"formats"`
}

// OptionSpec declares one platform-specific upload option.
type OptionSpec struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"` // "string" or "bool"
	Description string `json:"description"`
}

// VideoMeta is the user-facing metadata for an upload.
type VideoMeta struct {
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
	Privacy     string         `json:"privacy"`
	Options     map[string]any `json:"options,omitempty"`
}

// UploadRequest is one attempt to upload a file to an account.
type UploadRequest struct {
	AccountExternalID string
	AccountMeta       map[string]string
	Token             Token
	File              io.ReaderAt
	FileSize          int64
	Container         string
	Meta              VideoMeta
	Phase             string          // last persisted phase
	Resume            json.RawMessage // last persisted resume state
}

// ProgressUpdate reports upload progress. A changed Phase is persisted before the callback
// returns; byte-only updates are throttled.
type ProgressUpdate struct {
	BytesSent int64
	Phase     string
	Resume    json.RawMessage
}

// Progress receives progress updates. An error means the phase could not be persisted and
// the provider must not proceed to the next step.
type Progress func(ProgressUpdate) error

// Result is a finished upload.
type Result struct {
	RemoteID string
	URL      string
	Outcome  string
	Note     string
}

// Provider is a platform integration.
type Provider interface {
	Platform() string
	// RedirectHost is the loopback host registered with the platform ("127.0.0.1" or "localhost").
	RedirectHost() string
	// Refreshable reports whether tokens can be refreshed without user interaction.
	Refreshable() bool
	AuthURL(state, pkceVerifier, redirectURI string) string
	Exchange(ctx context.Context, code, pkceVerifier, redirectURI string) (Token, error)
	Refresh(ctx context.Context, tok Token) (Token, error)
	// Identities returns the stable login subject and the publish targets it can reach.
	Identities(ctx context.Context, tok Token) (subject string, ids []Identity, err error)
	Capabilities(identityMeta map[string]string) Capabilities
	Options() []OptionSpec
	ValidateMeta(meta VideoMeta) error
	Upload(ctx context.Context, req UploadRequest, progress Progress) (Result, error)
}

// CheckOptions rejects unknown option keys and values of the wrong kind.
func CheckOptions(specs []OptionSpec, opts map[string]any) error {
	for k, v := range opts {
		var spec *OptionSpec
		for i := range specs {
			if specs[i].Name == k {
				spec = &specs[i]
			}
		}
		if spec == nil {
			return fmt.Errorf("unknown option %q", k)
		}
		switch spec.Kind {
		case "bool":
			if _, ok := v.(bool); !ok {
				return fmt.Errorf("option %q must be a boolean", k)
			}
		case "string":
			if _, ok := v.(string); !ok {
				return fmt.Errorf("option %q must be a string", k)
			}
		default:
			return fmt.Errorf("option %q has unsupported kind %q", k, spec.Kind)
		}
	}
	return nil
}

// StringOption returns a string option or def.
func StringOption(opts map[string]any, name, def string) string {
	if v, ok := opts[name].(string); ok && v != "" {
		return v
	}
	return def
}

// BoolOption returns a bool option or def.
func BoolOption(opts map[string]any, name string, def bool) bool {
	if v, ok := opts[name].(bool); ok {
		return v
	}
	return def
}

// MIMEType returns the media type for a container format.
func MIMEType(container string) string {
	switch container {
	case FormatMOV:
		return "video/quicktime"
	case FormatWebM:
		return "video/webm"
	case FormatMKV:
		return "video/x-matroska"
	default:
		return "video/mp4"
	}
}

// PhaseAtLeast reports whether phase has reached min in the phase order.
func PhaseAtLeast(phase, min string) bool {
	return phaseRank(phase) >= phaseRank(min)
}

func phaseRank(p string) int {
	switch p {
	case PhaseTransferring:
		return 1
	case PhaseCommitting:
		return 2
	case PhaseProcessing:
		return 3
	case PhaseDone:
		return 4
	default:
		return 0
	}
}
