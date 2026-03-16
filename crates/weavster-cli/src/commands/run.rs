//! Run the Weavster runtime

use anyhow::{Context, Result};
use std::collections::HashMap;
use std::sync::Arc;
use weavster_codegen::{CompileOptions, Compiler};
use weavster_core::config::Config;
use weavster_runtime::Runtime;
use weavster_runtime::state::{PostgresStateStore, SqliteStateStore, StateStore};

/// Run the Weavster runtime
pub async fn run(
    config_path: &str,
    _flow: Option<&str>,
    _once: bool,
    profile: Option<&str>,
) -> Result<()> {
    tracing::info!("Loading configuration from {}", config_path);

    let config =
        Config::load_with_profile(config_path, profile).context("Failed to load configuration")?;

    tracing::info!("Project: {}", config.project.name);

    // Compile flows first
    let flows = config
        .load_flows()
        .context("Failed to load flow configurations")?;
    let mut flow_wasm_paths = HashMap::new();
    let cache_dir = config.base_path.join(".weavster/cache");
    let options = CompileOptions {
        output_dir: config.base_path.join(".weavster/output"),
        cache_dir: cache_dir.clone(),
        ..Default::default()
    };
    let compiler = Compiler::new(options);

    for flow in &flows {
        tracing::info!("Compiling flow: {}", flow.name);
        let flow_config_path = config
            .base_path
            .join("flows")
            .join(format!("{}.yaml", flow.name));
        let compile_ctx = compiler
            .compile_flow(&flow_config_path)
            .await
            .with_context(|| format!("Failed to compile flow: {}", flow.name))?;

        let wasm_cache_path = cache_dir.join(format!("{}.wasm", compile_ctx.hash));
        flow_wasm_paths.insert(flow.name.clone(), wasm_cache_path);
    }

    let state_store: Arc<dyn StateStore> = if let Some(pg_url) = std::env::var_os("WEAVSTER_PG_URL")
    {
        tracing::info!("Using Postgres state store from environment");
        let pg_url_str = pg_url
            .to_str()
            .ok_or_else(|| anyhow::anyhow!("WEAVSTER_PG_URL is not valid UTF-8"))?;
        Arc::new(
            PostgresStateStore::new(pg_url_str)
                .await
                .context("Failed to connect to Postgres")?,
        )
    } else {
        tracing::info!("Using SQLite state store for local development");
        let db_path = config.base_path.join(".weavster/data/local.db");
        if let Some(parent) = db_path.parent() {
            std::fs::create_dir_all(parent).context("Failed to create database directory")?;
        }
        let db_url = weavster_runtime::state::path_to_sqlite_url(&db_path);
        Arc::new(
            SqliteStateStore::new(&db_url)
                .await
                .context("Failed to connect to SQLite")?,
        )
    };

    let runtime = Runtime::new(config, state_store, flow_wasm_paths)
        .context("Failed to initialize WASM runtime")?;

    tracing::info!("Starting runtime (press Ctrl+C to stop)");

    let shutdown = async {
        tokio::signal::ctrl_c()
            .await
            .expect("Failed to install Ctrl+C handler");
        tracing::info!("Received shutdown signal");
    };

    tokio::select! {
        result = runtime.start() => {
            result.context("Runtime error")?;
        }
        _ = shutdown => {
            runtime.shutdown().await.context("Shutdown error")?;
        }
    }

    tracing::info!("Weavster stopped");
    Ok(())
}
