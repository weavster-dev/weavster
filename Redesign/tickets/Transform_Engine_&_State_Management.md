# Transform Engine & State Management

## Overview

Implement the transform engine that orchestrates WASM execution and manages state persistence. This integrates the WASM runtime with the database layer for tracking processed files, flow executions, and test results.

**Spec References:**
- `spec:3afb66cd-a148-48b8-8ce9-0bf5860c562d/e13dad9c-eda8-46d1-be4f-09ce88c29a23` - Component Architecture (Transform Engine, State Management), Data Model
- `spec:3afb66cd-a148-48b8-8ce9-0bf5860c562d/71f5f5ce-0ce5-44c1-826e-997166b1accd` - Flow 5 (Error Handling)

## Scope

**In Scope:**
- Implement Transform Engine in `file:crates/weavster-runtime/src/engine.rs`:
  - Integrate WASM runtime from Ticket 4
  - Jinja runtime evaluation for dynamic values (`{{ now() }}`)
  - Error handling policies (hierarchical: global → flow → transform)
  - Message processing pipeline (read → transform → write)
  - Filter execution (drop records when filter returns false)
- Implement State Management:
  - Switch from `sqlx` to `diesel` for database abstraction
  - SQLite implementation for dev server mode
  - Postgres implementation for prod server mode
  - Database migrations with `diesel_migrations`
  - Schema: `processed_files`, `bridge_messages`, `flow_executions`, `test_results`
  - File tracking: unique (path + content hash) per flow
- Update `file:crates/weavster-cli/src/local_db.rs` to use Diesel

**Out of Scope:**
- File watching (next ticket)
- Bridge connector implementation (included in schema, but runtime logic deferred)
- Testing framework (separate ticket)
- CLI run command (next ticket)

## Key Architectural Decisions

1. **Database Technology:** Diesel with SQLite/Postgres
   - Diesel for type-safe queries and migrations
   - SQLite for dev server mode (local testing)
   - Postgres for prod server mode (production deployments)
   - Use `diesel_async` for async runtime compatibility
   - If `diesel_async` issues arise, use `spawn_blocking` wrapper

2. **File Tracking:** Content-based deduplication
   - Track `(file_path, file_hash, flow_name)` tuple
   - Reprocess if file content changes (new hash)
   - Prevents duplicate processing of same content
   - Supports replay scenarios

3. **Error Handling:** Hierarchical configuration
   - Global defaults in `weavster.yaml`
   - Flow-level overrides in flow YAML
   - Transform-level overrides in transform config
   - Policies: `log_and_skip` (default) or `stop_on_error`
   - Retry configuration: max_attempts, backoff strategy

4. **Jinja Runtime Evaluation:**
   - Dynamic values evaluated per-message
   - Context includes: message data, flow metadata, timestamp
   - Functions: `now()`, `env`, custom functions
   - Errors in Jinja evaluation respect error handling policy

## Database Schema

```sql
-- processed_files table
CREATE TABLE processed_files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    flow_name TEXT NOT NULL,
    file_path TEXT NOT NULL,
    file_hash TEXT NOT NULL,
    processed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    record_count INTEGER NOT NULL,
    status TEXT NOT NULL, -- 'success' | 'failed' | 'partial'
    error_message TEXT,
    UNIQUE(flow_name, file_path, file_hash)
);

-- bridge_messages table (for bridge connector)
CREATE TABLE bridge_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    bridge_name TEXT NOT NULL,
    message_id TEXT NOT NULL UNIQUE,
    payload BLOB NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    processed_at TIMESTAMP,
    status TEXT NOT NULL, -- 'pending' | 'processing' | 'completed' | 'failed'
    retry_count INTEGER NOT NULL DEFAULT 0,
    error_message TEXT
);

-- flow_executions table
CREATE TABLE flow_executions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    flow_name TEXT NOT NULL,
    started_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMP,
    status TEXT NOT NULL, -- 'running' | 'completed' | 'failed'
    records_processed INTEGER NOT NULL DEFAULT 0,
    records_failed INTEGER NOT NULL DEFAULT 0,
    error_message TEXT
);

-- test_results table
CREATE TABLE test_results (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    test_name TEXT NOT NULL,
    flow_name TEXT NOT NULL,
    executed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    status TEXT NOT NULL, -- 'passed' | 'failed'
    duration_ms INTEGER NOT NULL,
    error_message TEXT,
    diff TEXT
);
```

## Implementation Details

### Transform Engine

```rust
pub struct TransformEngine {
    wasm_runtime: WasmRuntime,
    jinja_env: minijinja::Environment<'static>,
    db: Box<dyn StateStore>,
}

impl TransformEngine {
    pub async fn process_message(
        &self,
        flow: &Flow,
        message: &[u8],
    ) -> Result<Option<Vec<u8>>> {
        // 1. Evaluate dynamic Jinja values
        let context = self.build_jinja_context(flow, message)?;
        let flow_with_runtime_values = self.evaluate_jinja_runtime(flow, &context)?;

        // 2. Execute WASM transform
        let wasm_path = self.get_or_compile_wasm(flow)?;
        let result = self.wasm_runtime.execute(&wasm_path, message)?;

        // 3. Check if filter dropped the message
        if result.is_none() {
            return Ok(None); // Filter returned false
        }

        // 4. Apply error handling policy
        match result {
            Ok(output) => Ok(Some(output)),
            Err(e) => self.handle_error(flow, e),
        }
    }

    fn handle_error(&self, flow: &Flow, error: Error) -> Result<Option<Vec<u8>>> {
        let policy = flow.error_handling.on_error;
        match policy {
            ErrorPolicy::LogAndSkip => {
                log::warn!("Transform error: {}", error);
                Ok(None) // Skip this message
            }
            ErrorPolicy::StopOnError => {
                Err(error) // Propagate error, stop processing
            }
        }
    }
}
```

### State Store Trait

```rust
#[async_trait]
pub trait StateStore: Send + Sync {
    async fn mark_file_processed(
        &self,
        flow_name: &str,
        file_path: &str,
        file_hash: &str,
        record_count: usize,
    ) -> Result<()>;

    async fn is_file_processed(
        &self,
        flow_name: &str,
        file_path: &str,
        file_hash: &str,
    ) -> Result<bool>;

    async fn record_flow_execution(
        &self,
        flow_name: &str,
        records_processed: usize,
        records_failed: usize,
    ) -> Result<()>;

    // ... other methods
}

pub struct SqliteStateStore {
    pool: diesel::SqliteConnection,
}

pub struct PostgresStateStore {
    pool: diesel::PgConnection,
}
```

## Acceptance Criteria

- [ ] Transform engine integrates WASM runtime
- [ ] Jinja runtime evaluation works for dynamic values
- [ ] Error handling policies are respected (log_and_skip, stop_on_error)
- [ ] Hierarchical error handling config works (global → flow → transform)
- [ ] Filter transforms can drop messages (return None)
- [ ] Diesel is integrated with SQLite and Postgres support
- [ ] Database migrations run successfully
- [ ] File tracking prevents duplicate processing (path + hash)
- [ ] File tracking allows reprocessing when content changes
- [ ] State store trait is implemented for SQLite and Postgres
- [ ] Unit tests for transform engine logic
- [ ] Integration tests for database operations
- [ ] Integration tests for end-to-end message processing

## Testing Strategy

**Unit Tests:**
- Jinja runtime evaluation with various contexts
- Error handling policy application
- Filter result handling (drop vs keep)
- Hierarchical config resolution

**Integration Tests:**
- Process message through WASM transform
- Store processed file in database
- Check duplicate detection (same path + hash)
- Check reprocessing (same path, different hash)
- SQLite and Postgres implementations behave identically

**Database Tests:**
- Migrations run successfully
- CRUD operations work
- Unique constraints are enforced
- Queries are efficient (add indexes if needed)

## Dependencies

- **Depends on:** WASM Compilation Pipeline (Ticket 4) - for WASM runtime
- **Depends on:** Config Layer (Ticket 1) - for error handling config

## Estimated Effort

4-5 days (Phase 3 of implementation plan)

## Risk Mitigation

**Medium Risk:** Diesel async compatibility
- **Mitigation:** Use `diesel_async` if stable, otherwise `spawn_blocking`
- **Fallback:** Synchronous Diesel with blocking calls (acceptable for MVP)

**Low Risk:** Database migration complexity
- **Mitigation:** Keep schema simple for MVP, add complexity later
- **Fallback:** Manual schema creation if migrations fail
