//! Run the Weavster runtime

use anyhow::{Context, Result};
use weavster_core::Config;
use weavster_runtime::Runtime;

/// Run the Weavster runtime
pub async fn run(config_path: &str, _flow: Option<&str>, _once: bool) -> Result<()> {
    tracing::info!("Loading configuration from {}", config_path);

    let config = Config::load(config_path).context("Failed to load configuration")?;

    tracing::info!("Project: {}", config.project.name);

    let runtime = Runtime::new(config);

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
