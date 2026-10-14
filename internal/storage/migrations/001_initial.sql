CREATE TABLE providers (id TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE prompts (id TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE settings (id INTEGER PRIMARY KEY CHECK(id=1), value TEXT NOT NULL);
CREATE TABLE tasks (id TEXT PRIMARY KEY, status TEXT NOT NULL, text TEXT NOT NULL, translation TEXT NOT NULL, created_at TEXT NOT NULL, value TEXT NOT NULL);
CREATE INDEX tasks_created ON tasks(created_at DESC, id DESC);
CREATE INDEX tasks_status ON tasks(status);
