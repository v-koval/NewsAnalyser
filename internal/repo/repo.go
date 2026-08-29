package repo

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"newsanalyzer/internal/models"
)

type Repo struct{ Pool *pgxpool.Pool }

func New(p *pgxpool.Pool) *Repo { return &Repo{Pool: p} }

// -------- Users --------

func (r *Repo) CreateUser(ctx context.Context, email, hash string) (models.User, error) {
	var u models.User
	err := r.Pool.QueryRow(ctx,
		`INSERT INTO users(email,password_hash) VALUES($1,$2) RETURNING id,email,password_hash,created_at`,
		email, hash).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	return u, err
}

func (r *Repo) GetUserByEmail(ctx context.Context, email string) (models.User, error) {
	var u models.User
	err := r.Pool.QueryRow(ctx,
		`SELECT id,email,password_hash,created_at FROM users WHERE email=$1`, email).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	return u, err
}

func (r *Repo) GetUserByID(ctx context.Context, id string) (models.User, error) {
	var u models.User
	err := r.Pool.QueryRow(ctx,
		`SELECT id,email,password_hash,created_at FROM users WHERE id=$1`, id).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	return u, err
}

func (r *Repo) ListUsers(ctx context.Context) ([]models.User, error) {
	rows, err := r.Pool.Query(ctx, `SELECT id,email,password_hash,created_at FROM users ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.User{}
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, nil
}

func (r *Repo) UpdateUser(ctx context.Context, id, email, hash string) error {
	if hash == "" {
		_, err := r.Pool.Exec(ctx, `UPDATE users SET email=$2 WHERE id=$1`, id, email)
		return err
	}
	_, err := r.Pool.Exec(ctx, `UPDATE users SET email=$2,password_hash=$3 WHERE id=$1`, id, email, hash)
	return err
}

func (r *Repo) DeleteUser(ctx context.Context, id string) error {
	_, err := r.Pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, id)
	return err
}

func (r *Repo) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := r.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// -------- Refresh tokens --------

func (r *Repo) StoreRefresh(ctx context.Context, userID, tokenHash string, exp time.Time) error {
	_, err := r.Pool.Exec(ctx, `INSERT INTO refresh_tokens(user_id,token_hash,expires_at) VALUES($1,$2,$3)`, userID, tokenHash, exp)
	return err
}

func (r *Repo) FindRefresh(ctx context.Context, tokenHash string) (string, time.Time, error) {
	var uid string
	var exp time.Time
	err := r.Pool.QueryRow(ctx, `SELECT user_id,expires_at FROM refresh_tokens WHERE token_hash=$1`, tokenHash).Scan(&uid, &exp)
	return uid, exp, err
}

func (r *Repo) DeleteRefresh(ctx context.Context, tokenHash string) error {
	_, err := r.Pool.Exec(ctx, `DELETE FROM refresh_tokens WHERE token_hash=$1`, tokenHash)
	return err
}

func (r *Repo) DeleteUserRefresh(ctx context.Context, userID string) error {
	_, err := r.Pool.Exec(ctx, `DELETE FROM refresh_tokens WHERE user_id=$1`, userID)
	return err
}

// -------- Settings --------

func (r *Repo) GetSettings(ctx context.Context) (models.Settings, error) {
	var s models.Settings
	err := r.Pool.QueryRow(ctx, `SELECT cursor_api_key,cursor_repository,smtp_host,smtp_port,smtp_user,smtp_password,smtp_from,smtp_tls,processing_paused,keep_runs_days FROM settings WHERE id=1`).
		Scan(&s.CursorAPIKey, &s.CursorRepository, &s.SMTPHost, &s.SMTPPort, &s.SMTPUser, &s.SMTPPassword, &s.SMTPFrom, &s.SMTPTLS, &s.ProcessingPaused, &s.KeepRunsDays)
	return s, err
}

func (r *Repo) UpdateSettings(ctx context.Context, s models.Settings) error {
	_, err := r.Pool.Exec(ctx,
		`UPDATE settings SET cursor_api_key=$1,cursor_repository=$2,smtp_host=$3,smtp_port=$4,smtp_user=$5,smtp_password=$6,smtp_from=$7,smtp_tls=$8,processing_paused=$9,keep_runs_days=$10 WHERE id=1`,
		s.CursorAPIKey, s.CursorRepository, s.SMTPHost, s.SMTPPort, s.SMTPUser, s.SMTPPassword, s.SMTPFrom, s.SMTPTLS, s.ProcessingPaused, s.KeepRunsDays)
	return err
}

// -------- Digests --------

func scanDigest(row pgx.Row) (models.Digest, error) {
	var d models.Digest
	var sources, ignored, recipients, auto []byte
	err := row.Scan(&d.ID, &d.Name, &d.Topic, &sources, &ignored, &d.FrequencyHours, &recipients, &d.Language, &d.Kind, &d.Enabled, &d.LastRunAt, &d.NextRunAt, &auto, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return d, err
	}
	_ = json.Unmarshal(sources, &d.Sources)
	_ = json.Unmarshal(ignored, &d.IgnoredSources)
	_ = json.Unmarshal(recipients, &d.Recipients)
	_ = json.Unmarshal(auto, &d.AutoSources)
	return d, nil
}

const digestCols = `id,name,topic,sources,ignored_sources,frequency_hours,recipients,language,kind,enabled,last_run_at,next_run_at,auto_sources,created_at,updated_at`

func (r *Repo) CreateDigest(ctx context.Context, d models.Digest) (models.Digest, error) {
	src, _ := json.Marshal(orEmpty(d.Sources))
	ign, _ := json.Marshal(orEmpty(d.IgnoredSources))
	rec, _ := json.Marshal(orEmpty(d.Recipients))
	auto, _ := json.Marshal(orEmpty(d.AutoSources))
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO digests(name,topic,sources,ignored_sources,frequency_hours,recipients,language,kind,enabled,auto_sources)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING `+digestCols,
		d.Name, d.Topic, src, ign, d.FrequencyHours, rec, d.Language, d.Kind, d.Enabled, auto)
	return scanDigest(row)
}

func (r *Repo) UpdateDigest(ctx context.Context, d models.Digest) (models.Digest, error) {
	src, _ := json.Marshal(orEmpty(d.Sources))
	ign, _ := json.Marshal(orEmpty(d.IgnoredSources))
	rec, _ := json.Marshal(orEmpty(d.Recipients))
	row := r.Pool.QueryRow(ctx,
		`UPDATE digests SET name=$2,topic=$3,sources=$4,ignored_sources=$5,frequency_hours=$6,recipients=$7,language=$8,kind=$9,enabled=$10,updated_at=now()
		 WHERE id=$1 RETURNING `+digestCols,
		d.ID, d.Name, d.Topic, src, ign, d.FrequencyHours, rec, d.Language, d.Kind, d.Enabled)
	return scanDigest(row)
}

func (r *Repo) GetDigest(ctx context.Context, id string) (models.Digest, error) {
	row := r.Pool.QueryRow(ctx, `SELECT `+digestCols+` FROM digests WHERE id=$1`, id)
	return scanDigest(row)
}

func (r *Repo) ListDigests(ctx context.Context) ([]models.Digest, error) {
	rows, err := r.Pool.Query(ctx, `SELECT `+digestCols+` FROM digests ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Digest{}
	for rows.Next() {
		d, err := scanDigest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func (r *Repo) DeleteDigest(ctx context.Context, id string) error {
	_, err := r.Pool.Exec(ctx, `DELETE FROM digests WHERE id=$1`, id)
	return err
}

// LastRunPeriodTo returns period_to of the most recent SUCCESSFUL run
// (status ok or empty) for the given digest, or nil if there is none.
// Failed runs are excluded on purpose: their window must be re-covered
// by the next run instead of being silently skipped.
func (r *Repo) LastRunPeriodTo(ctx context.Context, digestID string) (*time.Time, error) {
	var t time.Time
	err := r.Pool.QueryRow(ctx,
		`SELECT period_to FROM digest_runs
		 WHERE digest_id=$1 AND status IN ('ok','empty')
		 ORDER BY period_to DESC LIMIT 1`,
		digestID).Scan(&t)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *Repo) SetDigestLastRun(ctx context.Context, id string, t time.Time) error {
	_, err := r.Pool.Exec(ctx, `UPDATE digests SET last_run_at=$2 WHERE id=$1`, id, t)
	return err
}

func (r *Repo) SetDigestNextRun(ctx context.Context, id string, t time.Time) error {
	_, err := r.Pool.Exec(ctx, `UPDATE digests SET next_run_at=$2 WHERE id=$1`, id, t)
	return err
}

func (r *Repo) AppendAutoSources(ctx context.Context, id string, newSources []string) error {
	if len(newSources) == 0 {
		return nil
	}
	d, err := r.GetDigest(ctx, id)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, s := range d.AutoSources {
		seen[s] = true
	}
	for _, s := range d.Sources {
		seen[s] = true
	}
	for _, s := range newSources {
		if !seen[s] {
			d.AutoSources = append(d.AutoSources, s)
			seen[s] = true
		}
	}
	raw, _ := json.Marshal(d.AutoSources)
	_, err = r.Pool.Exec(ctx, `UPDATE digests SET auto_sources=$2 WHERE id=$1`, id, raw)
	return err
}

// -------- Runs --------

func (r *Repo) CreateRun(ctx context.Context, run models.DigestRun) (models.DigestRun, error) {
	analyzed, _ := json.Marshal(orEmpty(run.AnalyzedSources))
	err := r.Pool.QueryRow(ctx,
		`INSERT INTO digest_runs(digest_id,digest_name,analyzed_sources,period_from,period_to,html,status,error)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id,processed_at`,
		run.DigestID, run.DigestName, analyzed, run.PeriodFrom, run.PeriodTo, run.HTML, run.Status, run.Error).
		Scan(&run.ID, &run.ProcessedAt)
	return run, err
}

func (r *Repo) StartRun(ctx context.Context, run models.DigestRun) (models.DigestRun, error) {
	analyzed, _ := json.Marshal(orEmpty(run.AnalyzedSources))
	run.Status = "processing"
	err := r.Pool.QueryRow(ctx,
		`INSERT INTO digest_runs(digest_id,digest_name,analyzed_sources,period_from,period_to,html,status)
		 VALUES($1,$2,$3,$4,$5,'',$6) RETURNING id,processed_at`,
		run.DigestID, run.DigestName, analyzed, run.PeriodFrom, run.PeriodTo, run.Status).
		Scan(&run.ID, &run.ProcessedAt)
	return run, err
}

func (r *Repo) FinishRun(ctx context.Context, run models.DigestRun) error {
	analyzed, _ := json.Marshal(orEmpty(run.AnalyzedSources))
	err := r.Pool.QueryRow(ctx,
		`UPDATE digest_runs
		 SET analyzed_sources=$2, html=$3, status=$4, error=$5, processed_at=now()
		 WHERE id=$1
		 RETURNING processed_at`,
		run.ID, analyzed, run.HTML, run.Status, run.Error).
		Scan(&run.ProcessedAt)
	return err
}

func (r *Repo) SetRunMail(ctx context.Context, runID, status, errText string) error {
	_, err := r.Pool.Exec(ctx, `UPDATE digest_runs SET mail_status=$2, mail_error=$3 WHERE id=$1`, runID, status, errText)
	return err
}

// FailStaleProcessing marks runs stuck in 'processing' (typically after a
// server restart mid-run) as failed. Returns the number of affected rows.
func (r *Repo) FailStaleProcessing(ctx context.Context) (int64, error) {
	tag, err := r.Pool.Exec(ctx,
		`UPDATE digest_runs SET status='error', error='прерван перезапуском сервера', processed_at=now() WHERE status='processing'`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (r *Repo) AddMaterial(ctx context.Context, runID string, m models.Material) error {
	_, err := r.Pool.Exec(ctx,
		`INSERT INTO digest_materials(run_id,url,title,summary_title,summary_text,full_text,image_url,local_image,position)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		runID, m.URL, m.Title, m.SummaryTitle, m.SummaryText, m.FullText, m.ImageURL, m.LocalImage, m.Position)
	return err
}

func (r *Repo) ListRuns(ctx context.Context) ([]models.DigestRun, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT id,digest_id,digest_name,analyzed_sources,processed_at,period_from,period_to,status,COALESCE(error,''),mail_status,mail_error FROM digest_runs ORDER BY processed_at DESC LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.DigestRun{}
	for rows.Next() {
		var run models.DigestRun
		var analyzed []byte
		if err := rows.Scan(&run.ID, &run.DigestID, &run.DigestName, &analyzed, &run.ProcessedAt, &run.PeriodFrom, &run.PeriodTo, &run.Status, &run.Error, &run.MailStatus, &run.MailError); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(analyzed, &run.AnalyzedSources)
		out = append(out, run)
	}
	return out, nil
}

func (r *Repo) GetRun(ctx context.Context, id string) (models.DigestRun, error) {
	var run models.DigestRun
	var analyzed []byte
	err := r.Pool.QueryRow(ctx,
		`SELECT id,digest_id,digest_name,analyzed_sources,processed_at,period_from,period_to,html,status,COALESCE(error,''),mail_status,mail_error FROM digest_runs WHERE id=$1`, id).
		Scan(&run.ID, &run.DigestID, &run.DigestName, &analyzed, &run.ProcessedAt, &run.PeriodFrom, &run.PeriodTo, &run.HTML, &run.Status, &run.Error, &run.MailStatus, &run.MailError)
	if err != nil {
		return run, err
	}
	_ = json.Unmarshal(analyzed, &run.AnalyzedSources)
	rows, err := r.Pool.Query(ctx,
		`SELECT id,url,title,summary_title,summary_text,full_text,COALESCE(image_url,''),COALESCE(local_image,''),position FROM digest_materials WHERE run_id=$1 ORDER BY position`, id)
	if err != nil {
		return run, err
	}
	defer rows.Close()
	for rows.Next() {
		var m models.Material
		if err := rows.Scan(&m.ID, &m.URL, &m.Title, &m.SummaryTitle, &m.SummaryText, &m.FullText, &m.ImageURL, &m.LocalImage, &m.Position); err != nil {
			return run, err
		}
		run.Materials = append(run.Materials, m)
	}
	return run, nil
}

// -------- Cleanup --------

func (r *Repo) ListOldRunIDs(ctx context.Context, before time.Time) ([]string, error) {
	rows, err := r.Pool.Query(ctx, `SELECT id FROM digest_runs WHERE processed_at < $1 AND status <> 'processing'`, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

func (r *Repo) DeleteRunsByID(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := r.Pool.Exec(ctx, `DELETE FROM digest_runs WHERE id = ANY($1)`, ids)
	return err
}

func (r *Repo) DeleteExpiredRefresh(ctx context.Context) (int64, error) {
	tag, err := r.Pool.Exec(ctx, `DELETE FROM refresh_tokens WHERE expires_at < now()`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

var ErrNotFound = errors.New("not found")
