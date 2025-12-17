//! Run the Weavster runtime

use anyhow::{Context, Result};
use sqlx::PgPool;
use weavster_core::Config;
use weavster_runtime::Runtime;

use crate::local_db::LocalDatabase;

/// Run the Weavster runtime
pub async fn run(config_path: &str, flow: Option<&str>, once: bool) -> Result<()> {
    tracing::info!("Loading configuration from {}", config_path);

    let config = Config::load(config_path).context("Failed to load configuration")?;

    tracing::info!("Project: {}", config.project.name);

    if let Some(flow_name) = flow {
        tracing::info!("Running single flow: {}", flow_name);
    }

    // Start local database if in local mode
    let pool = match config.project.runtime.mode {
        weavster_core::config::RuntimeMode::Local => {
            tracing::info!("Starting local database...");

            let local_db = LocalDatabase::new(&config.project.runtime.local)
                .await
                .context("Failed to start local database")?;

            let pool = local_db.pool().clone();

            // Run migrations
            tracing::info!("Running migrations...");
            // TODO: sqlx::migrate!("./migrations").run(&pool).await?;

            pool
        }
        weavster_core::config::RuntimeMode::Remote => {
            let url = config
                .project
                .runtime
                .remote
                .postgres_url
                .as_ref()
                .context("Remote mode requires postgres_url in configuration")?;

            tracing::info!("Connecting to remote database...");
            PgPool::connect(url)
                .await
                .context("Failed to connect to remote database")?
        }
    };

    // Create and start runtime
    let runtime = Runtime::new(config, pool);

    if once {
        tracing::info!("Running in single-shot mode");
        // TODO: Process one message and exit
    } else {
        tracing::info!("Starting runtime (press Ctrl+C to stop)");

        // Handle shutdown signal
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
    }

    tracing::info!("Weavster stopped");
    Ok(())
}
