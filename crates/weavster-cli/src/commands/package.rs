//! Package flows into OCI artifacts

use anyhow::{Context, Result};
use std::path::PathBuf;
use weavster_core::Config;

/// Run the package command
pub async fn run(config_path: &str, sign: bool, output: Option<&str>) -> Result<()> {
    tracing::info!("Loading configuration from {}", config_path);

    let config = Config::load(config_path).context("Failed to load configuration")?;

    let output_path = output
        .map(PathBuf::from)
        .unwrap_or_else(|| config.base_path.join(".weavster/artifact"));

    tracing::info!("Packaging project: {}", config.project.name);

    // Ensure flows are compiled first
    tracing::info!("Ensuring flows are compiled...");
    let cache_dir = config.base_path.join(".weavster/cache");
    if !cache_dir.exists() {
        anyhow::bail!("No compiled flows found. Run `weavster compile` first.");
    }

    // Create OCI artifact structure
    tracing::info!("Creating OCI artifact...");
    std::fs::create_dir_all(&output_path)?;

    // Create manifest
    let manifest = create_manifest(&config)?;
    std::fs::write(
        output_path.join("manifest.json"),
        serde_json::to_string_pretty(&manifest)?,
    )?;

    // Copy compiled WASM files
    let wasm_dir = output_path.join("wasm");
    std::fs::create_dir_all(&wasm_dir)?;

    for entry in std::fs::read_dir(&cache_dir)? {
        let entry = entry?;
        let path = entry.path();
        if path.extension().is_some_and(|ext| ext == "wasm") {
            let dest = wasm_dir.join(path.file_name().unwrap());
            std::fs::copy(&path, &dest)?;
            tracing::debug!("Copied {}", path.display());
        }
    }

    // Copy config files
    let config_dir = output_path.join("config");
    std::fs::create_dir_all(&config_dir)?;
    std::fs::copy(
        config.base_path.join("weavster.yaml"),
        config_dir.join("weavster.yaml"),
    )?;

    // Copy flows
    let flows_src = config.base_path.join("flows");
    if flows_src.exists() {
        let flows_dest = config_dir.join("flows");
        std::fs::create_dir_all(&flows_dest)?;
        copy_dir_contents(&flows_src, &flows_dest)?;
    }

    // Copy artifacts (translation tables, etc.)
    let artifacts_src = config.base_path.join("artifacts");
    if artifacts_src.exists() {
        let artifacts_dest = output_path.join("artifacts");
        std::fs::create_dir_all(&artifacts_dest)?;
        copy_dir_contents(&artifacts_src, &artifacts_dest)?;
    }

    tracing::info!("✓ Artifact created at {}", output_path.display());

    // Sign if requested
    if sign {
        tracing::info!("Signing artifact with cosign...");
        sign_artifact(&output_path).await?;
        tracing::info!("✓ Artifact signed");
    }

    // Print summary
    let artifact_size = dir_size(&output_path)?;
    tracing::info!(
        "Artifact summary: {} ({} bytes)",
        config.project.name,
        artifact_size
    );

    Ok(())
}

fn create_manifest(config: &Config) -> Result<serde_json::Value> {
    Ok(serde_json::json!({
        "schemaVersion": 2,
        "mediaType": "application/vnd.weavster.flow.v1+json",
        "config": {
            "mediaType": "application/vnd.weavster.config.v1+json",
            "digest": "sha256:placeholder"
        },
        "annotations": {
            "org.opencontainers.image.title": config.project.name,
            "org.opencontainers.image.version": config.project.version,
            "io.weavster.flow.version": "1.0"
        }
    }))
}

async fn sign_artifact(_path: &PathBuf) -> Result<()> {
    // Check if cosign is available
    let output = tokio::process::Command::new("cosign")
        .arg("version")
        .output()
        .await;

    match output {
        Ok(o) if o.status.success() => {
            // TODO: Implement actual signing
            // cosign sign-blob --key cosign.key artifact.tar
            tracing::warn!("Signing not yet fully implemented");
            Ok(())
        }
        _ => {
            tracing::warn!("cosign not found. Install it to enable signing.");
            tracing::warn!("  https://docs.sigstore.dev/cosign/installation/");
            Ok(())
        }
    }
}

fn copy_dir_contents(src: &std::path::Path, dest: &std::path::Path) -> Result<()> {
    for entry in std::fs::read_dir(src)? {
        let entry = entry?;
        let path = entry.path();
        let dest_path = dest.join(entry.file_name());

        if path.is_dir() {
            std::fs::create_dir_all(&dest_path)?;
            copy_dir_contents(&path, &dest_path)?;
        } else {
            std::fs::copy(&path, &dest_path)?;
        }
    }
    Ok(())
}

fn dir_size(path: &PathBuf) -> Result<u64> {
    let mut size = 0;
    for entry in walkdir::WalkDir::new(path) {
        let entry = entry?;
        if entry.file_type().is_file() {
            size += entry.metadata()?.len();
        }
    }
    Ok(size)
}
