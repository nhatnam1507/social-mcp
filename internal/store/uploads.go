package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/nhatnam1507/social-mcp/internal/ids"
)

// Upload statuses.
const (
	StatusQueued      = "queued"
	StatusUploading   = "uploading"
	StatusSucceeded   = "succeeded"
	StatusFailed      = "failed"
	StatusCancelled   = "cancelled"
	StatusNeedsReview = "needs_review"
)

// Upload phases. A job at PhaseCommitting or later must never repeat its publish step.
const (
	PhasePending      = "pending"
	PhaseTransferring = "transferring"
	PhaseCommitting   = "committing"
	PhaseProcessing   = "processing"
	PhaseDone         = "done"
)

// Upload is one job: one file to one account.
type Upload struct {
	ID              string
	AccountID       string
	Platform        string
	FilePath        string
	FileSize        int64
	FileMtime       time.Time
	FileFingerprint string
	Container       string
	Request         json.RawMessage
	Status          string
	Phase           string
	BytesSent       int64
	RemoteID        string
	RemoteURL       string
	Outcome         string
	Note            string
	Error           string
	ResumeState     json.RawMessage
	Attempts        int
	NotBefore       time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// UploadFields lists optional columns to set during a transition. Nil means unchanged;
// a zero NotBefore clears it.
type UploadFields struct {
	Phase       *string
	BytesSent   *int64
	RemoteID    *string
	RemoteURL   *string
	Outcome     *string
	Note        *string
	Error       *string
	ResumeState *json.RawMessage
	Attempts    *int
	NotBefore   *time.Time
}

// UploadFilter narrows ListUploads.
type UploadFilter struct {
	Status    string
	AccountID string
	Limit     int
}

const uploadCols = `id, account_id, platform, file_path, file_size, file_mtime, file_fingerprint, container, request, status, phase,
	bytes_sent, remote_id, remote_url, outcome, note, error, resume_state, attempts, not_before, created_at, updated_at`

func scanUpload(r rowScanner) (Upload, error) {
	var u Upload
	var mtime, created, updated int64
	var nb sql.NullInt64
	var req, resume string
	err := r.Scan(&u.ID, &u.AccountID, &u.Platform, &u.FilePath, &u.FileSize, &mtime, &u.FileFingerprint, &u.Container, &req,
		&u.Status, &u.Phase, &u.BytesSent, &u.RemoteID, &u.RemoteURL, &u.Outcome, &u.Note, &u.Error, &resume, &u.Attempts, &nb, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	if mtime != 0 {
		u.FileMtime = time.Unix(0, mtime)
	}
	u.Request = json.RawMessage(req)
	if resume != "" {
		u.ResumeState = json.RawMessage(resume)
	}
	u.NotBefore = fromMs(nb)
	u.CreatedAt, u.UpdatedAt = time.UnixMilli(created), time.UnixMilli(updated)
	return u, err
}

// InsertUpload creates a job and returns it with its ID.
func (s *Store) InsertUpload(u Upload) (Upload, error) {
	out, err := s.InsertUploads([]Upload{u})
	if err != nil {
		return u, err
	}
	return out[0], nil
}

// InsertUploads creates several jobs in one transaction.
func (s *Store) InsertUploads(us []Upload) ([]Upload, error) {
	tx, err := s.w.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := nowMs()
	for i := range us {
		u := &us[i]
		u.ID = ids.New("job")
		if u.Request == nil {
			u.Request = json.RawMessage("{}")
		}
		if _, err := tx.Exec(`INSERT INTO uploads(id, account_id, platform, file_path, file_size, file_mtime, file_fingerprint, container, request, status, phase, created_at, updated_at)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			u.ID, u.AccountID, u.Platform, u.FilePath, u.FileSize, unixNano(u.FileMtime), u.FileFingerprint, u.Container, string(u.Request), u.Status, u.Phase, now, now); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	out := make([]Upload, len(us))
	for i, u := range us {
		got, err := s.GetUpload(u.ID)
		if err != nil {
			return nil, err
		}
		out[i] = got
	}
	return out, nil
}

// CancelQueued cancels a queued job that has not reached its publish step. It reports
// whether the job was cancelled.
func (s *Store) CancelQueued(id, note string) (bool, error) {
	res, err := s.w.Exec("UPDATE uploads SET status = 'cancelled', note = ?, updated_at = ? WHERE id = ? AND status = 'queued' AND phase IN ('pending', 'transferring')", note, nowMs(), id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// GetUpload returns one job.
func (s *Store) GetUpload(id string) (Upload, error) {
	return scanUpload(s.r.QueryRow("SELECT "+uploadCols+" FROM uploads WHERE id = ?", id))
}

// ListUploads returns jobs, newest first.
func (s *Store) ListUploads(f UploadFilter) ([]Upload, error) {
	var where []string
	var args []any
	if f.Status != "" {
		where, args = append(where, "status = ?"), append(args, f.Status)
	}
	if f.AccountID != "" {
		where, args = append(where, "account_id = ?"), append(args, f.AccountID)
	}
	q := "SELECT " + uploadCols + " FROM uploads"
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.r.Query(q+" ORDER BY created_at DESC, rowid DESC LIMIT ?", append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Upload
	for rows.Next() {
		u, err := scanUpload(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// TransitionUpload moves a job from status `from` to `to` and sets the given fields, only if
// the job is still in `from`. It reports whether the update applied.
func (s *Store) TransitionUpload(id, from, to string, f UploadFields) (bool, error) {
	set := []string{"status = ?", "updated_at = ?"}
	args := []any{to, nowMs()}
	add := func(col string, v any) { set, args = append(set, col+" = ?"), append(args, v) }
	if f.Phase != nil {
		add("phase", *f.Phase)
	}
	if f.BytesSent != nil {
		add("bytes_sent", *f.BytesSent)
	}
	if f.RemoteID != nil {
		add("remote_id", *f.RemoteID)
	}
	if f.RemoteURL != nil {
		add("remote_url", *f.RemoteURL)
	}
	if f.Outcome != nil {
		add("outcome", *f.Outcome)
	}
	if f.Note != nil {
		add("note", *f.Note)
	}
	if f.Error != nil {
		add("error", *f.Error)
	}
	if f.ResumeState != nil {
		add("resume_state", string(*f.ResumeState))
	}
	if f.Attempts != nil {
		add("attempts", *f.Attempts)
	}
	if f.NotBefore != nil {
		add("not_before", ms(*f.NotBefore))
	}
	res, err := s.w.Exec("UPDATE uploads SET "+strings.Join(set, ", ")+" WHERE id = ? AND status = ?", append(args, id, from)...)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// ClaimNextUpload atomically moves the oldest eligible queued job to uploading. For platforms
// in serialized, a job is skipped while another job of the same account is uploading.
func (s *Store) ClaimNextUpload(now time.Time, serialized []string) (*Upload, error) {
	tx, err := s.w.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	q := `SELECT u.id FROM uploads u WHERE u.status = 'queued' AND (u.not_before IS NULL OR u.not_before <= ?)`
	args := []any{now.UnixMilli()}
	if len(serialized) > 0 {
		q += ` AND NOT (u.platform IN (` + strings.TrimSuffix(strings.Repeat("?,", len(serialized)), ",") + `)
			AND EXISTS (SELECT 1 FROM uploads o WHERE o.account_id = u.account_id AND o.status = 'uploading'))`
		for _, p := range serialized {
			args = append(args, p)
		}
	}
	var id string
	err = tx.QueryRow(q+" ORDER BY u.created_at, u.rowid LIMIT 1", args...).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec("UPDATE uploads SET status = 'uploading', updated_at = ? WHERE id = ? AND status = 'queued'", nowMs(), id); err != nil {
		return nil, err
	}
	u, err := scanUpload(tx.QueryRow("SELECT "+uploadCols+" FROM uploads WHERE id = ?", id))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &u, nil
}

// HasActiveUploads reports whether an account has queued or uploading jobs.
func (s *Store) HasActiveUploads(accountID string) (bool, error) {
	var n int
	err := s.r.QueryRow("SELECT COUNT(*) FROM uploads WHERE account_id = ? AND status IN ('queued', 'uploading')", accountID).Scan(&n)
	return n > 0, err
}

// CountActiveUploads counts queued or uploading jobs.
func (s *Store) CountActiveUploads() (int, error) {
	var n int
	err := s.r.QueryRow("SELECT COUNT(*) FROM uploads WHERE status IN ('queued', 'uploading')").Scan(&n)
	return n, err
}

// ResetInterrupted re-queues jobs left uploading by a previous process.
func (s *Store) ResetInterrupted() (int64, error) {
	res, err := s.w.Exec("UPDATE uploads SET status = 'queued', updated_at = ? WHERE status = 'uploading'", nowMs())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func unixNano(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixNano()
}
