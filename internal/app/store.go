package app

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schema string

type Store struct {
	Pool   *pgxpool.Pool
	Config Config
}

func OpenStore(ctx context.Context, c Config) (*Store, error) {
	pc, e := pgxpool.ParseConfig(c.DatabaseURL)
	if e != nil {
		return nil, fmt.Errorf("DATABASE_URL inválida")
	}
	// Several workers, each with progress/heartbeat updates, share this pool
	// with the HTTP API and the bot. Four connections caused avoidable waits.
	conns := c.DBMaxConns
	if conns < 4 {
		conns = 16
	}
	pc.MaxConns = int32(conns)
	pc.MinConns = 2
	pc.MaxConnLifetime = 30 * time.Minute
	pc.MaxConnIdleTime = 5 * time.Minute
	pc.HealthCheckPeriod = 30 * time.Second
	pc.ConnConfig.ConnectTimeout = 5 * time.Second
	p, e := pgxpool.NewWithConfig(ctx, pc)
	if e != nil {
		return nil, e
	}
	if e = p.Ping(ctx); e != nil {
		p.Close()
		return nil, fmt.Errorf("PostgreSQL no disponible")
	}
	if _, e = p.Exec(ctx, schema); e != nil {
		p.Close()
		return nil, e
	}
	return &Store{p, c}, nil
}

const columns = "id,user_id,chat_id,url,title,state,progress,options,error,files,created_at,updated_at,worker,stage,detail"

func scan(row pgx.Row) (Job, error) {
	var j Job
	var opts, files []byte
	e := row.Scan(&j.ID, &j.UserID, &j.ChatID, &j.URL, &j.Title, &j.State, &j.Progress, &opts, &j.Error, &files, &j.CreatedAt, &j.UpdatedAt, &j.Worker, &j.Stage, &j.Detail)
	if e != nil {
		return j, e
	}
	if e = json.Unmarshal(opts, &j.Options); e != nil {
		return j, e
	}
	if e = json.Unmarshal(files, &j.Files); e != nil {
		return j, e
	}
	return j, nil
}
func (s *Store) Create(ctx context.Context, user, chat int64, raw string, o Options) (Job, error) {
	if e := o.Validate(); e != nil {
		return Job{}, e
	}
	if o.UseCookies && !s.Config.Admins[user] {
		return Job{}, fmt.Errorf("cookies reservadas al administrador")
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return Job{}, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", user); e != nil {
		return Job{}, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO users(id) VALUES($1) ON CONFLICT DO NOTHING", user); e != nil {
		return Job{}, e
	}
	var global int
	if e = tx.QueryRow(ctx, "SELECT count(*) FROM jobs WHERE state IN ('queued','downloading','uploading')").Scan(&global); e != nil {
		return Job{}, e
	}
	queueLimit := s.Config.QueueLimit
	if queueLimit < 1 {
		queueLimit = 1000
	}
	if global >= queueLimit {
		return Job{}, fmt.Errorf("la cola del servidor está llena; reintenta más tarde")
	}
	var pending int
	if e = tx.QueryRow(ctx, "SELECT count(*) FROM jobs WHERE user_id=$1 AND state IN ('queued','downloading','uploading')", user).Scan(&pending); e != nil {
		return Job{}, e
	}
	if pending >= s.Config.MaxPending {
		return Job{}, fmt.Errorf("ya tienes %d descargas pendientes", pending)
	}
	if e = s.checkQuota(ctx, tx, user, raw, o); e != nil {
		return Job{}, e
	}
	b, _ := json.Marshal(o)
	j, e := scan(tx.QueryRow(ctx, "INSERT INTO jobs(id,user_id,chat_id,url,options,category) VALUES($1,$2,$3,$4,$5,$6) RETURNING "+columns, newID(), user, chat, raw, b, category(raw)))
	if e != nil {
		return j, e
	}
	return j, tx.Commit(ctx)
}
func (s *Store) List(ctx context.Context, user int64) ([]Job, error) {
	rows, e := s.Pool.Query(ctx, "SELECT "+columns+" FROM jobs WHERE user_id=$1 ORDER BY created_at DESC LIMIT 100", user)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	jobs := []Job{}
	for rows.Next() {
		j, e := scan(rows)
		if e != nil {
			return nil, e
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}
func (s *Store) Get(ctx context.Context, id string, user int64) (Job, error) {
	return scan(s.Pool.QueryRow(ctx, "SELECT "+columns+" FROM jobs WHERE id=$1 AND user_id=$2", id, user))
}

// Claim serializes dequeue operations with a transaction-scoped advisory lock.
// This makes the per-user active limit exact even when several workers ask for
// work at the same time, without serializing normal database traffic.
func (s *Store) Claim(ctx context.Context, worker string) (Job, error) {
	maxPerUser := s.Config.MaxActivePerUser
	if maxPerUser < 1 {
		maxPerUser = 1
	}
	admins := make([]int64, 0, len(s.Config.Admins))
	for id := range s.Config.Admins {
		admins = append(admins, id)
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return Job{}, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(819534763, 42)"); e != nil {
		return Job{}, e
	}
	j, e := scan(tx.QueryRow(ctx, `WITH candidate AS (
		SELECT j.id
		FROM jobs j
		WHERE j.state='queued'
		  AND (SELECT count(*) FROM jobs active WHERE active.user_id=j.user_id AND active.state IN ('downloading','uploading')) < $2
		ORDER BY
		  (j.user_id = ANY($3::bigint[]) OR EXISTS (SELECT 1 FROM subscriptions s WHERE s.user_id=j.user_id AND s.expires_at>now() AND NOT s.refunded)) DESC,
		  (j.created_at<now()-interval '30 minutes') DESC,
		  EXISTS (SELECT 1 FROM jobs active WHERE active.user_id=j.user_id AND active.state IN ('downloading','uploading')) ASC,
		  j.created_at ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED
	)
	UPDATE jobs SET state='downloading',worker=$1,updated_at=now()
	WHERE id=(SELECT id FROM candidate)
	RETURNING `+columns, worker, maxPerUser, admins))
	if e != nil {
		return j, e
	}
	return j, tx.Commit(ctx)
}
func (s *Store) Progress(ctx context.Context, id, worker string, n float64) error {
	_, e := s.Pool.Exec(ctx, "UPDATE jobs SET progress=$3,updated_at=now() WHERE id=$1 AND worker=$2 AND state='downloading'", id, worker, n)
	return e
}
func (s *Store) Finish(ctx context.Context, j Job, state, reason string, files []Output) error {
	if files == nil {
		files = []Output{}
	}
	b, _ := json.Marshal(files)
	_, e := s.Pool.Exec(ctx, "UPDATE jobs SET state=$3,error=$4,files=$5,stage='',detail='',progress=CASE WHEN $3='done' THEN 100 ELSE progress END,updated_at=now() WHERE id=$1 AND worker=$2 AND state IN ('downloading','uploading')", j.ID, j.Worker, state, reason, b)
	return e
}
func (s *Store) Cancel(ctx context.Context, id string, user int64) error {
	r, e := s.Pool.Exec(ctx, "UPDATE jobs SET state='cancelled',updated_at=now() WHERE id=$1 AND user_id=$2 AND state IN ('queued','downloading')", id, user)
	if e == nil && r.RowsAffected() == 0 {
		return fmt.Errorf("no se puede cancelar: finalizada, inexistente o ya se está enviando")
	}
	return e
}
func (s *Store) Uploading(ctx context.Context, j Job) (bool, error) {
	r, e := s.Pool.Exec(ctx, "UPDATE jobs SET state='uploading',updated_at=now() WHERE id=$1 AND worker=$2 AND state='downloading'", j.ID, j.Worker)
	return r.RowsAffected() == 1, e
}
func (s *Store) Settings(ctx context.Context, user int64) (Options, error) {
	var b []byte
	e := s.Pool.QueryRow(ctx, "SELECT settings FROM users WHERE id=$1", user).Scan(&b)
	var o Options
	if errors.Is(e, pgx.ErrNoRows) {
		e = nil
	}
	if e == nil && len(b) > 0 {
		e = json.Unmarshal(b, &o)
	}
	if e == nil {
		e = o.Validate()
	}
	return o, e
}
func (s *Store) SaveSettings(ctx context.Context, user int64, o Options) error {
	if e := o.Validate(); e != nil {
		return e
	}
	if o.UseCookies && !s.Config.Admins[user] {
		return fmt.Errorf("cookies reservadas al administrador")
	}
	b, _ := json.Marshal(o)
	_, e := s.Pool.Exec(ctx, "INSERT INTO users(id,settings) VALUES($1,$2) ON CONFLICT(id) DO UPDATE SET settings=excluded.settings", user, b)
	return e
}
func (s *Store) Offset(ctx context.Context) int64 {
	var n int64
	s.Pool.QueryRow(ctx, "SELECT value FROM bot_state WHERE key='offset'").Scan(&n)
	return n
}
func (s *Store) SetOffset(ctx context.Context, n int64) error {
	_, e := s.Pool.Exec(ctx, "INSERT INTO bot_state(key,value) VALUES('offset',$1) ON CONFLICT(key) DO UPDATE SET value=excluded.value", n)
	return e
}

type Folder struct {
	ID           string    `json:"id"`
	UserID       int64     `json:"userId"`
	Name         string    `json:"name"`
	ChatID       int64     `json:"chatId"`
	ChatTitle    string    `json:"chatTitle"`
	ChatUsername string    `json:"chatUsername"`
	ChatType     string    `json:"chatType"`
	CreatedAt    time.Time `json:"createdAt"`
}
type FolderRequest struct {
	RequestID int64     `json:"requestId"`
	UserID    int64     `json:"userId"`
	CreatedAt time.Time `json:"createdAt"`
}

const folderColumns = "id,user_id,name,chat_id,chat_title,chat_username,chat_type,created_at"

func (s *Store) ListFolders(ctx context.Context, user int64) ([]Folder, error) {
	rows, e := s.Pool.Query(ctx, "SELECT "+folderColumns+" FROM folders WHERE user_id=$1 ORDER BY created_at ASC", user)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	folders := []Folder{}
	for rows.Next() {
		var f Folder
		var created time.Time
		if e = rows.Scan(&f.ID, &f.UserID, &f.Name, &f.ChatID, &f.ChatTitle, &f.ChatUsername, &f.ChatType, &created); e != nil {
			return nil, e
		}
		f.CreatedAt = created
		folders = append(folders, f)
	}
	return folders, rows.Err()
}

func (s *Store) Folder(ctx context.Context, user int64, id string) (*Folder, error) {
	var f Folder
	var created time.Time
	e := s.Pool.QueryRow(ctx, "SELECT "+folderColumns+" FROM folders WHERE id=$1 AND user_id=$2", id, user).Scan(&f.ID, &f.UserID, &f.Name, &f.ChatID, &f.ChatTitle, &f.ChatUsername, &f.ChatType, &created)
	if e != nil {
		return nil, e
	}
	f.CreatedAt = created
	return &f, nil
}

// CreateFolder enforces the Premium limit of five folders per user with a
// transaction-scoped advisory lock and rejects reusing a chat already stored.
func (s *Store) CreateFolder(ctx context.Context, user int64, name, chatType string, chatID int64, chatTitle, chatUsername string) (*Folder, error) {
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", user); e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO users(id) VALUES($1) ON CONFLICT DO NOTHING", user); e != nil {
		return nil, e
	}
	var count int
	if e = tx.QueryRow(ctx, "SELECT count(*) FROM folders WHERE user_id=$1", user).Scan(&count); e != nil {
		return nil, e
	}
	if !s.Config.Admins[user] && count >= 5 {
		return nil, fmt.Errorf("ya tienes el máximo de 5 carpetas; elimina una antes de añadir otra")
	}
	if e = tx.QueryRow(ctx, "SELECT 1 FROM folders WHERE user_id=$1 AND chat_id=$2 LIMIT 1", user, chatID).Scan(&count); e != nil {
		return nil, e
	}
	if count > 0 {
		return nil, fmt.Errorf("esa carpeta ya está registrada")
	}
	f, e := scanFolderTx(tx.QueryRow(ctx, "INSERT INTO folders(user_id,name,chat_id,chat_title,chat_username,chat_type) VALUES($1,$2,$3,$4,$5,$6) RETURNING "+folderColumns, user, name, chatID, chatTitle, chatUsername, chatType))
	if e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return f, nil
}
func scanFolderTx(row pgx.Row) (*Folder, error) {
	var f Folder
	var created time.Time
	if e := row.Scan(&f.ID, &f.UserID, &f.Name, &f.ChatID, &f.ChatTitle, &f.ChatUsername, &f.ChatType, &created); e != nil {
		return nil, e
	}
	f.CreatedAt = created
	return &f, nil
}
func (s *Store) RenameFolder(ctx context.Context, user int64, id, name string) (*Folder, error) {
	if _, e := s.Pool.Exec(ctx, "UPDATE folders SET name=$3 WHERE id=$1 AND user_id=$2", id, name, user); e != nil {
		return nil, e
	}
	return s.Folder(ctx, user, id)
}
func (s *Store) DeleteFolder(ctx context.Context, user int64, id string) error {
	_, e := s.Pool.Exec(ctx, "DELETE FROM folders WHERE id=$1 AND user_id=$2", id, user)
	return e
}

func (s *Store) ListAdmins(ctx context.Context) ([]int64, error) {
	rows, e := s.Pool.Query(ctx, "SELECT user_id FROM admins ORDER BY created_at ASC")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func (s *Store) AddAdmin(ctx context.Context, user int64) error {
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "INSERT INTO users(id) VALUES($1) ON CONFLICT DO NOTHING", user); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO admins(user_id) VALUES($1) ON CONFLICT DO NOTHING", user); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) RemoveAdmin(ctx context.Context, user int64) error {
	_, e := s.Pool.Exec(ctx, "DELETE FROM admins WHERE user_id=$1", user)
	return e
}

func (s *Store) CreateFolderRequest(ctx context.Context, user int64, requestID int64) error {
	_, e := s.Pool.Exec(ctx, "INSERT INTO folder_requests(request_id,user_id) VALUES($1,$2)", requestID, user)
	return e
}
func (s *Store) ConsumeFolderRequest(ctx context.Context, requestID, user int64) error {
	_, e := s.Pool.Exec(ctx, "DELETE FROM folder_requests WHERE request_id=$1 AND user_id=$2", requestID, user)
	return e
}
func (s *Store) FolderRequest(ctx context.Context, requestID, user int64) (*FolderRequest, error) {
	var r FolderRequest
	var created time.Time
	e := s.Pool.QueryRow(ctx, "SELECT request_id,user_id,created_at FROM folder_requests WHERE request_id=$1 AND user_id=$2", requestID, user).Scan(&r.RequestID, &r.UserID, &created)
	if e != nil {
		return nil, e
	}
	r.CreatedAt = created
	return &r, nil
}
