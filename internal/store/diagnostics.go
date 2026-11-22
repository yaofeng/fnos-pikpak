package store

import (
	"context"
	"os"
	"time"
)

// JobCounts is how many transfer jobs a user has per status.
type JobCounts struct {
	Downloads map[string]int
	Uploads   map[string]int
}

// JobFailure is one job that ended with an error, for the diagnostics panel.
type JobFailure struct {
	Kind      string    `json:"kind"`
	ID        string    `json:"id"`
	Code      string    `json:"error_code"`
	Message   string    `json:"error_message"`
	UpdatedAt time.Time `json:"updated_at"`
}

// JobCounts groups one user's transfer jobs by status.
func (s *Store) JobCounts(ctx context.Context, uid int) (JobCounts, error) {
	out := JobCounts{Downloads: map[string]int{}, Uploads: map[string]int{}}
	for table, dst := range map[string]map[string]int{
		"download_jobs": out.Downloads,
		"upload_jobs":   out.Uploads,
	} {
		rows, err := s.db.QueryContext(ctx,
			`SELECT status, COUNT(*) FROM `+table+` WHERE uid = ? GROUP BY status`, uid)
		if err != nil {
			return JobCounts{}, err
		}
		for rows.Next() {
			var status string
			var n int
			if err := rows.Scan(&status, &n); err != nil {
				rows.Close()
				return JobCounts{}, err
			}
			dst[status] = n
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return JobCounts{}, err
		}
		rows.Close()
	}
	return out, nil
}

// RecentJobFailures returns the most recently updated failed jobs of one user,
// downloads and uploads mixed.
func (s *Store) RecentJobFailures(ctx context.Context, uid, limit int) ([]JobFailure, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT kind, id, error_code, error_message, updated_at FROM (
  SELECT 'download' AS kind, id, error_code, error_message, updated_at
    FROM download_jobs WHERE uid = ? AND error_code != ''
  UNION ALL
  SELECT 'upload' AS kind, id, error_code, error_message, updated_at
    FROM upload_jobs WHERE uid = ? AND error_code != ''
) ORDER BY updated_at DESC LIMIT ?`, uid, uid, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []JobFailure{}
	for rows.Next() {
		var f JobFailure
		var updated string
		if err := rows.Scan(&f.Kind, &f.ID, &f.Code, &f.Message, &updated); err != nil {
			return nil, err
		}
		t, err := time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			t, _ = time.Parse(time.RFC3339, updated)
		}
		f.UpdatedAt = t
		out = append(out, f)
	}
	return out, rows.Err()
}

// DeleteUserJobs drops every transfer job of one user. Unbinding calls it:
// the jobs reference a remote account that no longer exists for this user, so
// keeping them would leave rows that can never run. Files on disk are not
// touched.
func (s *Store) DeleteUserJobs(ctx context.Context, uid int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, table := range []string{"download_jobs", "upload_jobs"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE uid = ?`, uid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DBSize reports the size of the SQLite file on disk, for the diagnostics
// panel. The WAL file is not included because SQLite checkpoints it.
func (s *Store) DBSize() (int64, error) {
	info, err := os.Stat(s.path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}
