//! Connector management commands

use anyhow::Result;

/// List configured connectors
pub async fn list(_config_path: &str) -> Result<()> {
    tracing::info!("Listing connectors");
    // TODO: Load and list all connectors
    println!("Connectors: Not yet implemented");
    Ok(())
}

/// Test connector connectivity
pub async fn test(_config_path: &str, name: &str) -> Result<()> {
    tracing::info!("Testing connector: {}", name);
    // TODO: Attempt to connect and verify
    println!("Connector test: Not yet implemented");
    Ok(())
}
