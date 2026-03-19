# File Processing: Watching, Glob Patterns & Run Command

## Overview

Implement continuous file processing with glob pattern support, file watching, and the `weavster run` command. This enables the core use case: processing files as they arrive in a directory.

**Spec References:**
- `spec:3afb66cd-a148-48b8-8ce9-0bf5860c562d/e13dad9c-eda8-46d1-be4f-09ce88c29a23` - Component Architecture (File Watching Layer)
- `spec:3afb66cd-a148-48b8-8ce9-0bf5860c562d/71f5f5ce-0ce5-44c1-826e-997166b1accd` - Flow 1 (First-Time User), Flow 6 (Dev/Prod)

## Scope

**In Scope:**
- Implement File Watching Layer:
  - Glob pattern support for input paths (e.g., `./data/input/*.jsonl`)
  - File watching with `notify` crate
  - Stability debounce (wait until file size stable)
  - Configurable scan cadence (default 5000ms, min 500ms)
  - Sequential file processing (one file at a time per flow)
  - Post-processing actions: move, delete, leave (configurable)
  - Process existing files on startup, then watch for new files
- Implement `weavster run` CLI command:
  - Profile support (`--profile dev` or `--profile prod`)
  - Progress reporting (records processed, time elapsed)
  - Ctrl+C handling (graceful shutdown)
  - `--preview` flag (display output in terminal, don't write files)
  - `--limit N` flag (process only first N records)
- Enhance file connectors in `file:crates/weavster-core/src/connectors.rs`

**Out of Scope:**
- Concurrent file processing (sequential only for MVP)
- Bridge connector runtime logic (schema exists, but runtime deferred)
- Non-file connectors (Kafka, HTTP, etc.)
- Distributed mode (local only for MVP)

## Key Architectural Decisions

1. **File Watching:** `notify` crate with stability debounce
   - Watch directories for new files matching glob pattern
   - Debounce: wait until file size stable for N ms (default 1000ms)
   - Prevents processing partially-written files
   - Configurable scan cadence (default 5000ms, min 500ms)

2. **Processing Order:** Sequential
   - Process one file at a time per flow
   - Multiple flows run concurrently (one async task per flow)
   - Within a flow, files processed sequentially
   - Simpler than parallel, easier to debug

3. **Post-Processing:** Configurable actions
   ```yaml
   input:
     type: file
     path: "./data/input/*.jsonl"
     on_processed: move  # move | delete | leave
     processed_dir: "./data/processed/"  # if on_processed: move
   ```
   - Default: `leave` (read-only processing)
   - `move`: move to processed directory
   - `delete`: delete after successful processing
   - Tests always use `leave` (never modify fixtures)

4. **Startup Behavior:** Process existing + watch
   - On startup, process all existing files matching glob
   - Then watch for new files
   - Use database to track processed files (avoid reprocessing)

5. **Flow Scheduling:** Concurrent flows, sequential within flow
   - Each flow runs in its own async task
   - Flows don't block each other
   - Within a flow, process files one at a time

## Implementation Details

### File Watcher

```rust
use notify::{Watcher, RecursiveMode, Event};

pub struct FileWatcher {
    watcher: RecommendedWatcher,
    glob_pattern: String,
    debounce_ms: u64,
    scan_cadence_ms: u64,
}

impl FileWatcher {
    pub async fn watch(&mut self) -> Result<()> {
        // 1. Process existing files
        let existing_files = glob::glob(&self.glob_pattern)?;
        for file in existing_files {
            self.process_file(file?).await?;
        }

        // 2. Watch for new files
        let (tx, rx) = channel();
        self.watcher.watch(path, RecursiveMode::NonRecursive)?;

        loop {
            tokio::select! {
                event = rx.recv() => {
                    if let Some(path) = self.handle_event(event?) {
                        // Wait for file to stabilize
                        self.wait_for_stability(&path).await?;
                        self.process_file(path).await?;
                    }
                }
                _ = tokio::time::sleep(Duration::from_millis(self.scan_cadence_ms)) => {
                    // Periodic scan for missed files
                    self.scan_for_new_files().await?;
                }
            }
        }
    }

    async fn wait_for_stability(&self, path: &Path) -> Result<()> {
        let mut last_size = 0;
        loop {
            let size = fs::metadata(path)?.len();
            if size == last_size {
                tokio::time::sleep(Duration::from_millis(self.debounce_ms)).await;
                let new_size = fs::metadata(path)?.len();
                if new_size == size {
                    return Ok(()); // Stable
                }
            }
            last_size = size;
            tokio::time::sleep(Duration::from_millis(100)).await;
        }
    }
}
```

### Run Command

```rust
pub async fn run_command(profile: Option<String>, preview: bool, limit: Option<usize>) -> Result<()> {
    // Load config with profile
    let config = load_config_with_profile(".", profile.as_deref())?;

    // Initialize state store (SQLite or Postgres based on profile)
    let state_store = create_state_store(&config)?;

    // Initialize transform engine
    let engine = TransformEngine::new(state_store)?;

    // Start flows concurrently
    let mut tasks = vec![];
    for flow in config.flows {
        let engine = engine.clone();
        let task = tokio::spawn(async move {
            run_flow(&engine, &flow, preview, limit).await
        });
        tasks.push(task);
    }

    // Wait for Ctrl+C
    tokio::select! {
        _ = tokio::signal::ctrl_c() => {
            println!("\nShutting down gracefully...");
        }
        _ = futures::future::join_all(tasks) => {
            println!("All flows completed");
        }
    }

    Ok(())
}

async fn run_flow(
    engine: &TransformEngine,
    flow: &Flow,
    preview: bool,
    limit: Option<usize>,
) -> Result<()> {
    let mut watcher = FileWatcher::new(&flow.input)?;
    let mut processed = 0;

    watcher.watch(|file_path| async {
        // Check if already processed
        let hash = compute_file_hash(&file_path)?;
        if engine.is_file_processed(&flow.name, &file_path, &hash).await? {
            return Ok(()); // Skip
        }

        // Process file
        let records = read_jsonl(&file_path)?;
        for record in records {
            if let Some(limit) = limit {
                if processed >= limit {
                    return Ok(());
                }
            }

            let output = engine.process_message(flow, &record).await?;

            if preview {
                println!("{}", serde_json::to_string_pretty(&output)?);
            } else {
                write_output(flow, &output)?;
            }

            processed += 1;
        }

        // Mark as processed
        engine.mark_file_processed(&flow.name, &file_path, &hash, records.len()).await?;

        // Post-processing
        match flow.input.on_processed {
            PostProcessAction::Move => move_file(&file_path, &flow.input.processed_dir)?,
            PostProcessAction::Delete => fs::remove_file(&file_path)?,
            PostProcessAction::Leave => {},
        }

        Ok(())
    }).await
}
```

## Acceptance Criteria

- [ ] File watcher supports glob patterns (e.g., `*.jsonl`)
- [ ] Stability debounce prevents processing partial files
- [ ] Scan cadence is configurable (default 5000ms, min 500ms)
- [ ] Existing files are processed on startup
- [ ] New files are detected and processed
- [ ] Post-processing actions work (move, delete, leave)
- [ ] `weavster run --profile dev` uses dev profile
- [ ] `weavster run --preview` displays output without writing
- [ ] `weavster run --limit 10` processes only 10 records
- [ ] Ctrl+C gracefully shuts down
- [ ] Progress reporting shows records processed
- [ ] Multiple flows run concurrently
- [ ] Files within a flow are processed sequentially
- [ ] Integration tests verify end-to-end file processing

## Testing Strategy

**Unit Tests:**
- Glob pattern matching
- File stability detection
- Post-processing actions
- Hash computation

**Integration Tests:**
- Process existing files on startup
- Detect and process new files
- Stability debounce works (simulate partial write)
- Post-processing moves/deletes files correctly
- Duplicate detection prevents reprocessing
- Content change triggers reprocessing
- Multiple flows run concurrently
- Ctrl+C shutdown is graceful

**Performance Tests:**
- Measure throughput (records/second)
- Verify no memory leaks during long runs
- Test with large files (1M+ records)

## Dependencies

- **Depends on:** Transform Engine & State (Ticket 5) - for processing and state tracking
- **Depends on:** Config Layer (Ticket 1) - for profile support

## Estimated Effort

4-5 days (Phase 4 of implementation plan)

## Risk Mitigation

**High Risk:** File watching reliability across platforms
- **Mitigation:** Extensive testing on Linux, macOS, Windows
- **Fallback:** Polling mode if `notify` fails on certain filesystems

**Medium Risk:** Partial file detection
- **Mitigation:** Stability debounce + configurable wait time
- **Fallback:** Document atomic rename convention for producers
