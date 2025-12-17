//! Show project status command

use anyhow::Result;

/// Run the status command
pub async fn run(_config_path: &str) -> Result<()> {
    tracing::info!("Project status");

    // TODO: Show:
    // - Project info
    // - Number of flows
    // - Number of connectors
    // - Local database status
    // - Recent job statistics

    println!("Status: Not yet implemented");
    Ok(())
}
