//! Validate configuration command

use anyhow::{Context, Result};
use weavster_core::Config;

/// Run the validate command
pub async fn run(config_path: &str) -> Result<()> {
    tracing::info!("Validating configuration: {}", config_path);

    let config = Config::load(config_path).context("Failed to load configuration")?;

    tracing::info!("✓ Project: {}", config.project.name);
    tracing::info!("✓ Version: {}", config.project.version);
    tracing::info!("✓ Runtime mode: {:?}", config.project.runtime.mode);

    // TODO: Validate flows
    // TODO: Validate connectors
    // TODO: Validate expressions in transforms

    tracing::info!("✓ Configuration is valid");
    Ok(())
}
