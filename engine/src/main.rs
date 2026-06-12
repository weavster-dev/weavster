//! Weavster engine — the thin Rust production runtime (RFC 0003).
//!
//! Runs a compiled artifact (`weavster compile` output): loads `manifest.json`,
//! JIT-compiles each flow module once, and drives every pipeline concurrently
//! over the Javy stdin/stdout ABI. See `docs/ENGINE_PLAN.md` (E3) and
//! `docs/ARTIFACT_SPEC.md` for the contract.
//!
//! Boots from a mounted `weavster.yaml` (default `/etc/weavster/weavster.yaml`,
//! `-c/--config` to override) and resolves the artifact by convention next to
//! it — see `config.rs` and Engine Plan E5.

mod config;
mod connector;
mod connectors;
mod host;
mod log;
mod manifest;
mod registry;
mod runner;

use std::path::Path;
use std::process::ExitCode;

async fn run(artifact_dir: &Path) -> anyhow::Result<bool> {
    let manifest = manifest::load(artifact_dir)?;
    let report = runner::run(artifact_dir, &manifest).await?;

    for (pipeline, error) in &report.failures {
        eprintln!("✗ {pipeline}: {error}");
    }
    let total = manifest.pipelines.len();
    let ran = total - report.failures.len();
    eprintln!(
        "{ran}/{total} pipelines ran ({} documents)",
        report.documents
    );
    Ok(report.failures.is_empty())
}

fn main() -> ExitCode {
    let boot = match config::parse(std::env::args().skip(1)) {
        Ok(config::Cli::Run(boot)) => boot,
        Ok(config::Cli::Help) => {
            println!("{}", config::USAGE);
            return ExitCode::SUCCESS;
        }
        Err(err) => {
            eprintln!("✗ {err:#}");
            return ExitCode::FAILURE;
        }
    };

    // The mounted config is the boot anchor: refuse to start if it is absent,
    // rather than silently running whatever artifact happens to sit nearby.
    if !boot.config.exists() {
        eprintln!(
            "✗ no weavster.yaml at {} — mount your project config there or pass -c <path>",
            boot.config.display()
        );
        return ExitCode::FAILURE;
    }

    let runtime = match tokio::runtime::Runtime::new() {
        Ok(rt) => rt,
        Err(err) => {
            eprintln!("✗ cannot start the async runtime: {err}");
            return ExitCode::FAILURE;
        }
    };

    match runtime.block_on(run(&boot.artifact)) {
        Ok(true) => ExitCode::SUCCESS,
        Ok(false) => ExitCode::FAILURE,
        Err(err) => {
            eprintln!("✗ {err:#}");
            ExitCode::FAILURE
        }
    }
}
