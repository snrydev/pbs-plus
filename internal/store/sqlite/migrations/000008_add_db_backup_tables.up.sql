CREATE TABLE IF NOT EXISTS database_jobs (
  id TEXT PRIMARY KEY,
  store TEXT NOT NULL ,
  target TEXT NOT NULL,
  schedule TEXT,
  comment TEXT,
  notification_mode TEXT,
  namespace TEXT,
  current_pid TEXT,
  last_run_upid TEXT,
  last_successful_upid TEXT,
  retry INTEGER,
  retry_interval INTEGER
);

CREATE TABLE IF NOT EXISTS database_targets (
  name TEXT PRIMARY KEY,
  type TEXT NOT NULL,
  username TEXT,
  password TEXT,
  host TEXT,
  port INTEGER,
  db_name TEXT
);
