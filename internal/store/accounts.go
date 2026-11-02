package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/nhatnam1507/social-mcp/internal/ids"
)

// Credential and account statuses.
const (
	StatusOK          = "ok"
	StatusNeedsReauth = "needs_reauth"
)

// Credential is one OAuth grant (one login) for a platform.
type Credential struct {
	ID               string
	Platform         string
	Subject          string
	SealedToken      []byte
	ExpiresAt        time.Time
	RefreshExpiresAt time.Time
	Refreshable      bool
	Status           string
	Version          int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Account is one publish target (channel, Page, profile) reachable through a credential.
type Account struct {
	ID                    string
	Platform              string
	CredentialID          string
	ExternalID            string
	DisplayName           string
	SealedAccountToken    []byte
	AccountTokenExpiresAt time.Time
	Status                string
	Meta                  map[string]string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

const credentialCols = "id, platform, subject, sealed_token, expires_at, refresh_expires_at, refreshable, status, version, created_at, updated_at"

type rowScanner interface{ Scan(...any) error }

func scanCredential(r rowScanner) (Credential, error) {
	var c Credential
	var exp, rexp sql.NullInt64
	var created, updated int64
	err := r.Scan(&c.ID, &c.Platform, &c.Subject, &c.SealedToken, &exp, &rexp, &c.Refreshable, &c.Status, &c.Version, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	c.ExpiresAt, c.RefreshExpiresAt = fromMs(exp), fromMs(rexp)
	c.CreatedAt, c.UpdatedAt = time.UnixMilli(created), time.UnixMilli(updated)
	return c, err
}

const accountCols = "id, platform, credential_id, external_id, display_name, sealed_account_token, account_token_expires_at, status, meta, created_at, updated_at"

func scanAccount(r rowScanner) (Account, error) {
	var a Account
	var exp sql.NullInt64
	var meta string
	var created, updated int64
	err := r.Scan(&a.ID, &a.Platform, &a.CredentialID, &a.ExternalID, &a.DisplayName, &a.SealedAccountToken, &exp, &a.Status, &meta, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, err
	}
	a.AccountTokenExpiresAt = fromMs(exp)
	a.CreatedAt, a.UpdatedAt = time.UnixMilli(created), time.UnixMilli(updated)
	if err := json.Unmarshal([]byte(meta), &a.Meta); err != nil {
		return a, err
	}
	return a, nil
}

// UpsertLogin stores a credential (matched by platform+subject) and its accounts (matched
// by platform+external_id) in one transaction, keeping existing IDs and resetting status.
func (s *Store) UpsertLogin(c Credential, accounts []Account) (Credential, []Account, error) {
	tx, err := s.w.Begin()
	if err != nil {
		return c, nil, err
	}
	defer tx.Rollback()
	now := nowMs()

	existing, err := scanCredential(tx.QueryRow("SELECT "+credentialCols+" FROM credentials WHERE platform = ? AND subject = ?", c.Platform, c.Subject))
	switch {
	case err == nil:
		c.ID = existing.ID
		_, err = tx.Exec(`UPDATE credentials SET sealed_token = ?, expires_at = ?, refresh_expires_at = ?, refreshable = ?,
			status = 'ok', version = version + 1, updated_at = ? WHERE id = ?`,
			c.SealedToken, ms(c.ExpiresAt), ms(c.RefreshExpiresAt), c.Refreshable, now, c.ID)
	case errors.Is(err, ErrNotFound):
		c.ID = ids.New("cred")
		_, err = tx.Exec(`INSERT INTO credentials(id, platform, subject, sealed_token, expires_at, refresh_expires_at, refreshable, status, version, created_at, updated_at)
			VALUES(?, ?, ?, ?, ?, ?, ?, 'ok', 1, ?, ?)`,
			c.ID, c.Platform, c.Subject, c.SealedToken, ms(c.ExpiresAt), ms(c.RefreshExpiresAt), c.Refreshable, now, now)
	}
	if err != nil {
		return c, nil, err
	}

	out := make([]Account, 0, len(accounts))
	for _, a := range accounts {
		a.Platform, a.CredentialID = c.Platform, c.ID
		if a.Meta == nil {
			a.Meta = map[string]string{}
		}
		meta, err := json.Marshal(a.Meta)
		if err != nil {
			return c, nil, err
		}
		var id string
		err = tx.QueryRow("SELECT id FROM accounts WHERE platform = ? AND external_id = ?", a.Platform, a.ExternalID).Scan(&id)
		switch {
		case err == nil:
			a.ID = id
			_, err = tx.Exec(`UPDATE accounts SET credential_id = ?, display_name = ?, sealed_account_token = ?, account_token_expires_at = ?,
				status = 'ok', meta = ?, updated_at = ? WHERE id = ?`,
				a.CredentialID, a.DisplayName, a.SealedAccountToken, ms(a.AccountTokenExpiresAt), string(meta), now, a.ID)
		case errors.Is(err, sql.ErrNoRows):
			a.ID = ids.New("acc")
			_, err = tx.Exec(`INSERT INTO accounts(id, platform, credential_id, external_id, display_name, sealed_account_token, account_token_expires_at, status, meta, created_at, updated_at)
				VALUES(?, ?, ?, ?, ?, ?, ?, 'ok', ?, ?, ?)`,
				a.ID, a.Platform, a.CredentialID, a.ExternalID, a.DisplayName, a.SealedAccountToken, ms(a.AccountTokenExpiresAt), string(meta), now, now)
		}
		if err != nil {
			return c, nil, err
		}
		a.Status = StatusOK
		out = append(out, a)
	}
	if err := tx.Commit(); err != nil {
		return c, nil, err
	}
	stored, err := s.GetCredential(c.ID)
	return stored, out, err
}

// GetCredential returns one credential.
func (s *Store) GetCredential(id string) (Credential, error) {
	return scanCredential(s.r.QueryRow("SELECT "+credentialCols+" FROM credentials WHERE id = ?", id))
}

// ListCredentials returns all credentials.
func (s *Store) ListCredentials() ([]Credential, error) {
	rows, err := s.r.Query("SELECT " + credentialCols + " FROM credentials ORDER BY created_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Credential
	for rows.Next() {
		c, err := scanCredential(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpdateCredentialToken replaces the sealed token only if the stored version still equals
// expectedVersion. It reports whether the update applied.
func (s *Store) UpdateCredentialToken(id string, expectedVersion int64, sealed []byte, expiresAt, refreshExpiresAt time.Time) (bool, error) {
	res, err := s.w.Exec(`UPDATE credentials SET sealed_token = ?, expires_at = ?, refresh_expires_at = COALESCE(?, refresh_expires_at),
		status = 'ok', version = version + 1, updated_at = ? WHERE id = ? AND version = ?`,
		sealed, ms(expiresAt), ms(refreshExpiresAt), nowMs(), id, expectedVersion)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// SetCredentialStatus sets a credential's status.
func (s *Store) SetCredentialStatus(id, status string) error {
	_, err := s.w.Exec("UPDATE credentials SET status = ?, updated_at = ? WHERE id = ?", status, nowMs(), id)
	return err
}

// DeleteCredential removes a credential that no account references.
func (s *Store) DeleteCredential(id string) error {
	_, err := s.w.Exec("DELETE FROM credentials WHERE id = ? AND NOT EXISTS (SELECT 1 FROM accounts WHERE credential_id = ?)", id, id)
	return err
}

// GetAccount returns one account.
func (s *Store) GetAccount(id string) (Account, error) {
	return scanAccount(s.r.QueryRow("SELECT "+accountCols+" FROM accounts WHERE id = ?", id))
}

// ListAccounts returns accounts, optionally filtered by platform.
func (s *Store) ListAccounts(platform string) ([]Account, error) {
	q := "SELECT " + accountCols + " FROM accounts"
	var args []any
	if platform != "" {
		q += " WHERE platform = ?"
		args = append(args, platform)
	}
	rows, err := s.r.Query(q+" ORDER BY platform, display_name", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetAccountStatus sets an account's status.
func (s *Store) SetAccountStatus(id, status string) error {
	_, err := s.w.Exec("UPDATE accounts SET status = ?, updated_at = ? WHERE id = ?", status, nowMs(), id)
	return err
}

// DeleteAccount removes an account and, when it was the last one, its credential.
func (s *Store) DeleteAccount(id string) error {
	tx, err := s.w.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var credID string
	if err := tx.QueryRow("SELECT credential_id FROM accounts WHERE id = ?", id).Scan(&credID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if _, err := tx.Exec("DELETE FROM accounts WHERE id = ?", id); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM credentials WHERE id = ? AND NOT EXISTS (SELECT 1 FROM accounts WHERE credential_id = ?)", credID, credID); err != nil {
		return err
	}
	return tx.Commit()
}
