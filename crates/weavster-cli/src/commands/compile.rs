//! Compile flows to WASM

use anyhow::{Context, Result};
use std::path::Path;
use weavster_codegen::{CompileOptions, Compiler};
use weavster_core::Config;

/// Run the compile command
pub async fn run(config_path: &str, flow: Option<&str>, debug: bool, force: bool) -> Result<()> {
    tracing::info!("Loading configuration from {}", config_path);

    let config = Config::load(config_path).context("Failed to load configuration")?;
    let flows_dir = config.base_path.join("flows");

    let options = CompileOptions {
        output_dir: config.base_path.join(".weavster/output"),
        cache_dir: config.base_path.join(".weavster/cache"),
        debug,
        force,
        ..Default::default()
    };

    let compiler = Compiler::new(options);

    if let Some(flow_name) = flow {
        // Compile single flow
        let flow_path = flows_dir.join(format!("{}.yaml", flow_name));
        if !flow_path.exists() {
            let flow_path_yml = flows_dir.join(format!("{}.yml", flow_name));
            if flow_path_yml.exists() {
                compile_flow(&compiler, &flow_path_yml).await?;
            } else {
                anyhow::bail!("Flow not found: {}", flow_name);
            }
        } else {
            compile_flow(&compiler, &flow_path).await?;
        }
    } else {
        // Compile all flows
        tracing::info!("Compiling all flows in {}", flows_dir.display());

        let results = compiler
            .compile_all(&flows_dir)
            .await
            .context("Failed to compile flows")?;

        tracing::info!("Compiled {} flows:", results.len());
        for compiled in &results {
            tracing::info!(
                "  ✓ {} ({} bytes, hash: {}...)",
                compiled.name,
                compiled.size(),
                &compiled.hash[..8]
            );
        }
    }

    tracing::info!("Compilation complete");
    Ok(())
}

async fn compile_flow(compiler: &Compiler, path: &Path) -> Result<()> {
    tracing::info!("Compiling {}", path.display());

    let compiled = compiler
        .compile_flow(path)
        .await
        .context("Compilation failed")?;

    tracing::info!(
        "✓ {} ({} bytes, hash: {}...)",
        compiled.name,
        compiled.size(),
        &compiled.hash[..8]
    );

    Ok(())
}
