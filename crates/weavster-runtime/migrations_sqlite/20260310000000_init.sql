CREATE TABLE processed_files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    flow_name TEXT NOT NULL,
    file_path TEXT NOT NULL,
    file_hash TEXT NOT NULL,
    processed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    record_count BIGINT NOT NULL,
    status TEXT NOT NULL,
    error_message TEXT,
    UNIQUE(flow_name, file_path, file_hash)
);

CREATE TABLE bridge_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    bridge_name TEXT NOT NULL,
    message_id TEXT NOT NULL UNIQUE,
    payload BLOB NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    processed_at TIMESTAMP,
    status TEXT NOT NULL,
    retry_count INTEGER NOT NULL DEFAULT 0,
    error_message TEXT
);

CREATE TABLE flow_executions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    flow_name TEXT NOT NULL,
    started_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMP,
    status TEXT NOT NULL,
    records_processed BIGINT NOT NULL DEFAULT 0,
    records_failed BIGINT NOT NULL DEFAULT 0,
    error_message TEXT
);

CREATE TABLE test_results (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    test_name TEXT NOT NULL,
    flow_name TEXT NOT NULL,
    executed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    status TEXT NOT NULL,
    duration_ms INTEGER NOT NULL,
    error_message TEXT,
    diff TEXT
);
