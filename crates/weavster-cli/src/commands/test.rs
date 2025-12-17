//! Test runner command

use anyhow::Result;

/// Run tests
pub async fn run(_config_path: &str, pattern: Option<&str>) -> Result<()> {
    if let Some(p) = pattern {
        tracing::info!("Running tests matching: {}", p);
    } else {
        tracing::info!("Running all tests");
    }

    // TODO: Discover and run tests
    // - Load test fixtures
    // - Run flows with test input
    // - Compare output to expected

    println!("Tests: Not yet implemented");
    Ok(())
}
