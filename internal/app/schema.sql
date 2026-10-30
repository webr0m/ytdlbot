CREATE TABLE IF NOT EXISTS users (
 id BIGINT PRIMARY KEY, settings JSONB NOT NULL DEFAULT '{}', created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS jobs (
 id TEXT PRIMARY KEY, user_id BIGINT NOT NULL REFERENCES users(id), chat_id BIGINT NOT NULL,
 url TEXT NOT NULL, title TEXT NOT NULL DEFAULT '', options JSONB NOT NULL, category TEXT NOT NULL DEFAULT 'social',
 state TEXT NOT NULL DEFAULT 'queued' CHECK (state IN ('queued','downloading','uploading','done','failed','cancelled')),
 progress DOUBLE PRECISION NOT NULL DEFAULT 0, error TEXT NOT NULL DEFAULT '', files JSONB NOT NULL DEFAULT '[]',
 worker TEXT NOT NULL DEFAULT '', created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS jobs_queue ON jobs(created_at) WHERE state='queued';
CREATE INDEX IF NOT EXISTS jobs_user ON jobs(user_id,created_at DESC);
CREATE INDEX IF NOT EXISTS jobs_user_active ON jobs(user_id) WHERE state IN ('downloading','uploading');
CREATE INDEX IF NOT EXISTS jobs_active_heartbeat ON jobs(updated_at) WHERE state IN ('downloading','uploading');
CREATE INDEX IF NOT EXISTS jobs_finished_retention ON jobs(updated_at) WHERE state IN ('done','failed','cancelled');
CREATE TABLE IF NOT EXISTS bot_state (key TEXT PRIMARY KEY, value BIGINT NOT NULL);
CREATE TABLE IF NOT EXISTS invoices (
 payload TEXT PRIMARY KEY, user_id BIGINT NOT NULL REFERENCES users(id), stars INTEGER NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS subscriptions (
 charge_id TEXT PRIMARY KEY, user_id BIGINT NOT NULL REFERENCES users(id), payload TEXT NOT NULL,
 stars INTEGER NOT NULL, expires_at TIMESTAMPTZ NOT NULL, cancelled BOOLEAN NOT NULL DEFAULT false,
 refunded BOOLEAN NOT NULL DEFAULT false, paid_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS subscriptions_user ON subscriptions(user_id,expires_at DESC);
ALTER TABLE invoices ADD COLUMN IF NOT EXISTS approved_at TIMESTAMPTZ;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS stage TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS detail TEXT NOT NULL DEFAULT '';
CREATE TABLE IF NOT EXISTS folders (
  id TEXT PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  chat_id BIGINT NOT NULL,
  chat_title TEXT NOT NULL DEFAULT '',
  chat_username TEXT NOT NULL DEFAULT '',
  chat_type TEXT NOT NULL DEFAULT 'channel',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (user_id, chat_id)
);
CREATE INDEX IF NOT EXISTS folders_user ON folders(user_id, created_at);
CREATE TABLE IF NOT EXISTS folder_requests (
  request_id BIGINT PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS folder_requests_user ON folder_requests(user_id, created_at);
