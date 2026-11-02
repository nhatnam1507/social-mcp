package oauth

import (
	"errors"
	"sync"
	"time"

	"github.com/nhatnam1507/social-mcp/internal/ids"
)

// Session statuses.
const (
	StatusPending   = "pending"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusExpired   = "expired"
)

// ErrUnknownState means the callback state is unknown, already used, or expired.
var ErrUnknownState = errors.New("unknown or expired login session")

const sessionTTL = 10 * time.Minute

// Session is one pending or finished login.
type Session struct {
	ID               string
	Platform         string
	State            string
	Verifier         string
	RedirectURI      string
	ReauthExternalID string
	Status           string
	AccountIDs       []string
	Error            string
	CreatedAt        time.Time
	used             bool
}

// Sessions is an in-memory registry of logins started by the CLI.
type Sessions struct {
	mu      sync.Mutex
	byID    map[string]*Session
	byState map[string]*Session
	now     func() time.Time
}

// NewSessions returns an empty registry.
func NewSessions() *Sessions {
	return &Sessions{byID: map[string]*Session{}, byState: map[string]*Session{}, now: time.Now}
}

// Begin starts a login and returns a copy of its session.
func (s *Sessions) Begin(platform, redirectURI, reauthExternalID string) Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gc()
	sess := &Session{
		ID:               ids.New("auth"),
		Platform:         platform,
		State:            newState(),
		Verifier:         NewVerifier(),
		RedirectURI:      redirectURI,
		ReauthExternalID: reauthExternalID,
		Status:           StatusPending,
		CreatedAt:        s.now(),
	}
	s.byID[sess.ID] = sess
	s.byState[sess.State] = sess
	return *sess
}

// Take consumes a callback state. Each state can be taken once, within the TTL.
func (s *Sessions) Take(state string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.byState[state]
	if !ok || sess.used || s.expired(sess) {
		return Session{}, ErrUnknownState
	}
	sess.used = true
	return *sess, nil
}

// Complete records the result of a login.
func (s *Sessions) Complete(id string, accountIDs []string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.byID[id]
	if !ok {
		return
	}
	if err != nil {
		sess.Status, sess.Error = StatusFailed, err.Error()
		return
	}
	sess.Status, sess.AccountIDs = StatusSucceeded, accountIDs
}

// Get returns a copy of a session.
func (s *Sessions) Get(id string) (Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.byID[id]
	if !ok {
		return Session{}, false
	}
	out := *sess
	if out.Status == StatusPending && s.expired(sess) {
		out.Status = StatusExpired
	}
	return out, true
}

func (s *Sessions) expired(sess *Session) bool {
	return s.now().Sub(sess.CreatedAt) > sessionTTL
}

// gc drops sessions well past their TTL.
func (s *Sessions) gc() {
	for id, sess := range s.byID {
		if s.now().Sub(sess.CreatedAt) > 2*sessionTTL {
			delete(s.byID, id)
			delete(s.byState, sess.State)
		}
	}
}
