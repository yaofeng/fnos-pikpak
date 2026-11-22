package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// DownloadJob is one file being pulled from PikPak to the NAS. DestPath is the
// final path; the worker writes DestPath+".pikpak-part" until it verifies the
// size and renames.
type DownloadJob struct {
	ID           string    `json:"id"`
	UID          int       `json:"-"`
	FileID       string    `json:"file_id"`
	FileName     string    `json:"file_name"`
	DestDir      string    `json:"dest_dir"`
	DestPath     string    `json:"dest_path"`
	Size         int64     `json:"size"`
	Downloaded   int64     `json:"downloaded"`
	Status       string    `json:"status"`
	ErrorCode    string    `json:"error_code,omitempty"`
	ErrorMessage string    `json:"error_message,omitempty"`
	LinkExpireAt string    `json:"link_expire_at,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (s *Store) PutDownloadJob(ctx context.Context, j DownloadJob) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO download_jobs(id, uid, file_id, file_name, dest_dir, dest_path, size, downloaded,
  status, error_code, error_message, link_expire_at, created_at, updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET
  file_name=excluded.file_name,
  dest_dir=excluded.dest_dir,
  dest_path=excluded.dest_path,
  size=excluded.size,
  downloaded=excluded.downloaded,
  status=excluded.status,
  error_code=excluded.error_code,
  error_message=excluded.error_message,
  link_expire_at=excluded.link_expire_at,
  updated_at=excluded.updated_at`,
		j.ID, j.UID, j.FileID, j.FileName, j.DestDir, j.DestPath, j.Size, j.Downloaded,
		j.Status, j.ErrorCode, j.ErrorMessage, j.LinkExpireAt, now, now)
	return err
}

// StartDownloadJob moves a queued job to running and reports whether it did,
// so a worker that lost a race against a pause or a delete leaves the row
// alone. Progress and terminal writes are update-only: a job deleted while it
// runs is never inserted again by its worker.
func (s *Store) StartDownloadJob(ctx context.Context, uid int, id string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
UPDATE download_jobs SET status='running', error_code='', error_message='', updated_at=?
WHERE uid = ? AND id = ? AND status = 'queued'`,
		time.Now().UTC().Format(time.RFC3339Nano), uid, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (s *Store) SetDownloadProgress(ctx context.Context, uid int, id string, downloaded int64) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE download_jobs SET downloaded=?, updated_at=? WHERE uid = ? AND id = ?`,
		downloaded, time.Now().UTC().Format(time.RFC3339Nano), uid, id)
	return err
}

func (s *Store) FinishDownloadJob(ctx context.Context, uid int, id, status, code, message string, downloaded int64) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE download_jobs SET status=?, error_code=?, error_message=?, downloaded=?, updated_at=?
WHERE uid = ? AND id = ?`,
		status, code, message, downloaded, time.Now().UTC().Format(time.RFC3339Nano), uid, id)
	return err
}

func (s *Store) GetDownloadJob(ctx context.Context, uid int, id string) (DownloadJob, error) {
	var (
		j                DownloadJob
		created, updated string
	)
	err := s.db.QueryRowContext(ctx, `
SELECT id, uid, file_id, file_name, dest_dir, dest_path, size, downloaded,
       status, error_code, error_message, link_expire_at, created_at, updated_at
FROM download_jobs WHERE uid = ? AND id = ?`, uid, id).
		Scan(&j.ID, &j.UID, &j.FileID, &j.FileName, &j.DestDir, &j.DestPath, &j.Size, &j.Downloaded,
			&j.Status, &j.ErrorCode, &j.ErrorMessage, &j.LinkExpireAt, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return DownloadJob{}, ErrNotFound
	}
	if err != nil {
		return DownloadJob{}, err
	}
	j.CreatedAt = parseTime(created)
	j.UpdatedAt = parseTime(updated)
	return j, nil
}

// ListDownloadJobs returns the user's jobs newest first. With statuses given it
// filters to those states.
func (s *Store) ListDownloadJobs(ctx context.Context, uid int, statuses ...string) ([]DownloadJob, error) {
	query := `
SELECT id, uid, file_id, file_name, dest_dir, dest_path, size, downloaded,
       status, error_code, error_message, link_expire_at, created_at, updated_at
FROM download_jobs WHERE uid = ?`
	args := []any{uid}
	if len(statuses) > 0 {
		placeholders := make([]string, len(statuses))
		for i, st := range statuses {
			placeholders[i] = "?"
			args = append(args, st)
		}
		query += " AND status IN (" + strings.Join(placeholders, ",") + ")"
	}
	query += " ORDER BY created_at DESC, id DESC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []DownloadJob{}
	for rows.Next() {
		var (
			j                DownloadJob
			created, updated string
		)
		if err := rows.Scan(&j.ID, &j.UID, &j.FileID, &j.FileName, &j.DestDir, &j.DestPath,
			&j.Size, &j.Downloaded, &j.Status, &j.ErrorCode, &j.ErrorMessage, &j.LinkExpireAt,
			&created, &updated); err != nil {
			return nil, err
		}
		j.CreatedAt = parseTime(created)
		j.UpdatedAt = parseTime(updated)
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *Store) DeleteDownloadJob(ctx context.Context, uid int, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM download_jobs WHERE uid = ? AND id = ?`, uid, id)
	return err
}

// ResetRunningDownloadJobs runs at startup: a job left running has no worker,
// but its .part file allows a resume, so it goes back to the queue.
func (s *Store) ResetRunningDownloadJobs(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE download_jobs SET status = 'queued', updated_at = ? WHERE status = 'running'`,
		time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// UploadJob is one file being pushed to PikPak. LocalPath is an absolute path
// already validated against the user's fnOS access.
type UploadJob struct {
	ID           string    `json:"id"`
	UID          int       `json:"-"`
	LocalPath    string    `json:"local_path"`
	ParentID     string    `json:"parent_id"`
	RemoteName   string    `json:"remote_name"`
	Size         int64     `json:"size"`
	Uploaded     int64     `json:"uploaded"`
	Status       string    `json:"status"`
	ErrorCode    string    `json:"error_code,omitempty"`
	ErrorMessage string    `json:"error_message,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (s *Store) PutUploadJob(ctx context.Context, j UploadJob) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO upload_jobs(id, uid, local_path, parent_id, remote_name, size, uploaded,
  status, error_code, error_message, created_at, updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET
  local_path=excluded.local_path,
  parent_id=excluded.parent_id,
  remote_name=excluded.remote_name,
  size=excluded.size,
  uploaded=excluded.uploaded,
  status=excluded.status,
  error_code=excluded.error_code,
  error_message=excluded.error_message,
  updated_at=excluded.updated_at`,
		j.ID, j.UID, j.LocalPath, j.ParentID, j.RemoteName, j.Size, j.Uploaded,
		j.Status, j.ErrorCode, j.ErrorMessage, now, now)
	return err
}

// StartUploadJob and SetUploadProgress mirror the download helpers; see
// StartDownloadJob for why the claims are conditional.
func (s *Store) StartUploadJob(ctx context.Context, uid int, id string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
UPDATE upload_jobs SET status='running', error_code='', error_message='', updated_at=?
WHERE uid = ? AND id = ? AND status = 'queued'`,
		time.Now().UTC().Format(time.RFC3339Nano), uid, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (s *Store) SetUploadProgress(ctx context.Context, uid int, id string, uploaded int64) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE upload_jobs SET uploaded=?, updated_at=? WHERE uid = ? AND id = ?`,
		uploaded, time.Now().UTC().Format(time.RFC3339Nano), uid, id)
	return err
}

func (s *Store) FinishUploadJob(ctx context.Context, uid int, id, status, code, message string, uploaded int64) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE upload_jobs SET status=?, error_code=?, error_message=?, uploaded=?, updated_at=?
WHERE uid = ? AND id = ?`,
		status, code, message, uploaded, time.Now().UTC().Format(time.RFC3339Nano), uid, id)
	return err
}

func (s *Store) GetUploadJob(ctx context.Context, uid int, id string) (UploadJob, error) {
	var (
		j                UploadJob
		created, updated string
	)
	err := s.db.QueryRowContext(ctx, `
SELECT id, uid, local_path, parent_id, remote_name, size, uploaded,
       status, error_code, error_message, created_at, updated_at
FROM upload_jobs WHERE uid = ? AND id = ?`, uid, id).
		Scan(&j.ID, &j.UID, &j.LocalPath, &j.ParentID, &j.RemoteName, &j.Size, &j.Uploaded,
			&j.Status, &j.ErrorCode, &j.ErrorMessage, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return UploadJob{}, ErrNotFound
	}
	if err != nil {
		return UploadJob{}, err
	}
	j.CreatedAt = parseTime(created)
	j.UpdatedAt = parseTime(updated)
	return j, nil
}

func (s *Store) ListUploadJobs(ctx context.Context, uid int, statuses ...string) ([]UploadJob, error) {
	query := `
SELECT id, uid, local_path, parent_id, remote_name, size, uploaded,
       status, error_code, error_message, created_at, updated_at
FROM upload_jobs WHERE uid = ?`
	args := []any{uid}
	if len(statuses) > 0 {
		placeholders := make([]string, len(statuses))
		for i, st := range statuses {
			placeholders[i] = "?"
			args = append(args, st)
		}
		query += " AND status IN (" + strings.Join(placeholders, ",") + ")"
	}
	query += " ORDER BY created_at DESC, id DESC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []UploadJob{}
	for rows.Next() {
		var (
			j                UploadJob
			created, updated string
		)
		if err := rows.Scan(&j.ID, &j.UID, &j.LocalPath, &j.ParentID, &j.RemoteName,
			&j.Size, &j.Uploaded, &j.Status, &j.ErrorCode, &j.ErrorMessage,
			&created, &updated); err != nil {
			return nil, err
		}
		j.CreatedAt = parseTime(created)
		j.UpdatedAt = parseTime(updated)
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *Store) DeleteUploadJob(ctx context.Context, uid int, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM upload_jobs WHERE uid = ? AND id = ?`, uid, id)
	return err
}

func (s *Store) ResetRunningUploadJobs(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE upload_jobs SET status = 'queued', updated_at = ? WHERE status = 'running'`,
		time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// QueuedDownloadJobs returns queued jobs across all users, oldest first, so
// the transfer scheduler can fill its global slots fairly.
func (s *Store) QueuedDownloadJobs(ctx context.Context, limit int) ([]DownloadJob, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, uid, file_id, file_name, dest_dir, dest_path, size, downloaded,
       status, error_code, error_message, link_expire_at, created_at, updated_at
FROM download_jobs WHERE status = 'queued' ORDER BY created_at, id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DownloadJob{}
	for rows.Next() {
		var (
			j                DownloadJob
			created, updated string
		)
		if err := rows.Scan(&j.ID, &j.UID, &j.FileID, &j.FileName, &j.DestDir, &j.DestPath,
			&j.Size, &j.Downloaded, &j.Status, &j.ErrorCode, &j.ErrorMessage, &j.LinkExpireAt,
			&created, &updated); err != nil {
			return nil, err
		}
		j.CreatedAt = parseTime(created)
		j.UpdatedAt = parseTime(updated)
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *Store) QueuedUploadJobs(ctx context.Context, limit int) ([]UploadJob, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, uid, local_path, parent_id, remote_name, size, uploaded,
       status, error_code, error_message, created_at, updated_at
FROM upload_jobs WHERE status = 'queued' ORDER BY created_at, id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UploadJob{}
	for rows.Next() {
		var (
			j                UploadJob
			created, updated string
		)
		if err := rows.Scan(&j.ID, &j.UID, &j.LocalPath, &j.ParentID, &j.RemoteName,
			&j.Size, &j.Uploaded, &j.Status, &j.ErrorCode, &j.ErrorMessage,
			&created, &updated); err != nil {
			return nil, err
		}
		j.CreatedAt = parseTime(created)
		j.UpdatedAt = parseTime(updated)
		out = append(out, j)
	}
	return out, rows.Err()
}
