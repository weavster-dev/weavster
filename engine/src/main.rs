//! Weavster engine — the thin Rust production runtime (RFC 0003).
//!
//! Runs a compiled artifact (`weavster compile` output): loads `manifest.json`,
//! JIT-compiles each flow module once, and drives every pipeline concurrently
//! over the Javy stdin/stdout ABI. See `docs/ENGINE_PLAN.md` (E3) and
//! `docs/ARTIFACT_SPEC.md` for the contract.
//!
//! Usage: `weavster-engine <artifact-dir>` (E5 adds the `weavster.yaml` boot).

mod host;
mod log;
mod manifest;
mod runner;

use std::path::Path;
use std::process::ExitCode;

fn run(artifact_dir: &Path) -> anyhow::Result<bool> {
    let manifest = manifest::load(artifact_dir)?;
    let report = runner::run(artifact_dir, &manifest)?;

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
    // E5 replaces this with the mounted-`weavster.yaml` boot (-c/--config);
    // until then the artifact directory is explicit, never an implicit cwd.
    let Some(artifact_dir) = std::env::args().nth(1) else {
        eprintln!("usage: weavster-engine <artifact-dir>");
        return ExitCode::FAILURE;
    };

    match run(Path::new(&artifact_dir)) {
        Ok(true) => ExitCode::SUCCESS,
        Ok(false) => ExitCode::FAILURE,
        Err(err) => {
            eprintln!("✗ {err:#}");
            ExitCode::FAILURE
        }
    }
}
