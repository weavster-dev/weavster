//! Flow management commands

use anyhow::Result;

/// List all flows
pub async fn list(_config_path: &str) -> Result<()> {
    tracing::info!("Listing flows");
    // TODO: Load and list all flows
    println!("Flows: Not yet implemented");
    Ok(())
}

/// Show flow details
pub async fn show(_config_path: &str, name: &str) -> Result<()> {
    tracing::info!("Showing flow: {}", name);
    // TODO: Load and display flow details
    println!("Flow details: Not yet implemented");
    Ok(())
}

/// Create a new flow
pub async fn new(_config_path: &str, name: &str) -> Result<()> {
    tracing::info!("Creating flow: {}", name);
    // TODO: Create flow scaffold
    println!("Create flow: Not yet implemented");
    Ok(())
}
