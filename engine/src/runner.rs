//! Per-pipeline run loop (Engine Plan E3 slice 3).
//!
//! Each pipeline is `source → transform → sink` behind a FIFO queue with
//! concurrency 1 (documents stay in input order); pipelines run concurrently
//! with each other on plain threads. Error scoping per RFC 0002/0003: startup
//! errors abort the run; per-document failures fail a bounded run and would
//! log-and-move-on on a live stream (every E3 source is bounded — files).

use crate::host::{FlowModule, Host, InputEnvelope};
use crate::log;
use crate::manifest::{Manifest, Pipeline};
use anyhow::{Context, Result, anyhow, bail};
use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::sync::Arc;

pub struct RunReport {
    /// Pipeline name → error message, for pipelines that failed.
    pub failures: Vec<(String, String)>,
    pub documents: usize,
}

/// Load every flow the manifest references (deduplicated), then run all
/// pipelines concurrently. The connector root is the artifact directory.
pub fn run(artifact_dir: &Path, manifest: &Manifest) -> Result<RunReport> {
    let host = Host::new()?;
    let mut flows: HashMap<String, Arc<FlowModule>> = HashMap::new();
    for pipeline in &manifest.pipelines {
        if !flows.contains_key(&pipeline.flow) {
            let module = host
                .load_flow(artifact_dir, &pipeline.flow)
                .with_context(|| format!("pipeline \"{}\"", pipeline.name))?;
            flows.insert(pipeline.flow.clone(), Arc::new(module));
        }
    }

    let results: Vec<(String, Result<usize>)> = std::thread::scope(|scope| {
        let handles: Vec<_> = manifest
            .pipelines
            .iter()
            .map(|pipeline| {
                let flow = Arc::clone(&flows[&pipeline.flow]);
                let handle =
                    scope.spawn(move || run_pipeline(artifact_dir, pipeline, flow.as_ref()));
                (pipeline.name.clone(), handle)
            })
            .collect();
        handles
            .into_iter()
            .map(|(name, handle)| {
                let result = handle
                    .join()
                    .unwrap_or_else(|_| Err(anyhow!("pipeline thread panicked")));
                (name, result)
            })
            .collect()
    });

    let mut failures = Vec::new();
    let mut documents = 0;
    for (name, result) in results {
        match result {
            Ok(count) => documents += count,
            Err(err) => failures.push((name, format!("{err:#}"))),
        }
    }
    Ok(RunReport {
        failures,
        documents,
    })
}

/// One pipeline: resolve the source glob, run each document through the flow
/// in order, write each result to the sink. Returns the document count.
fn run_pipeline(artifact_dir: &Path, pipeline: &Pipeline, flow: &FlowModule) -> Result<usize> {
    // Startup: resolve inputs before transforming anything.
    let inputs = resolve_glob(artifact_dir, &pipeline.source.glob)
        .with_context(|| format!("source glob \"{}\"", pipeline.source.glob))?;
    if inputs.is_empty() {
        bail!("source glob \"{}\" matched no files", pipeline.source.glob);
    }
    let sink_path = artifact_dir.join(&pipeline.sink.path);

    let mut documents = 0;
    for input_path in inputs {
        documents += 1;
        let payload = std::fs::read_to_string(&input_path)
            .with_context(|| format!("cannot read {}", input_path.display()))?;

        let result = flow
            .run(&InputEnvelope {
                r#in: &pipeline.source.format,
                out: &pipeline.sink.format,
                payload: &payload,
            })
            .with_context(|| format!("document {documents} ({})", input_path.display()))?;

        if !result.ok {
            let error = result.error.as_ref();
            let stage = error.map_or("unknown", |e| e.stage.as_str());
            let error_type = error
                .and_then(|e| e.error_type.as_deref())
                .unwrap_or("unknown");
            let message = error
                .and_then(|e| e.message.as_deref())
                .unwrap_or("(no message)");
            log::error(&pipeline.name, documents, stage, error_type, message);
            // Every E3 source is bounded (files), so a poison document fails
            // the run. A live stream would log-and-move-on here instead.
            bail!("document {documents}: {stage}: {message}");
        }

        let output = result
            .payload
            .context("ok envelope is missing its payload")?;
        if let Some(parent) = sink_path.parent() {
            std::fs::create_dir_all(parent)?;
        }
        // One sink path, overwritten per document (last write wins) — the TS
        // file connector's semantics. Per-document naming for multi-match
        // globs is an E4 sink-semantics decision.
        std::fs::write(&sink_path, output)
            .with_context(|| format!("cannot write {}", sink_path.display()))?;
        log::done(&pipeline.name, documents);
    }
    Ok(documents)
}

/// Expand the source glob against the connector root, in sorted (input) order.
/// E2 emits a literal file path as a one-match glob; real patterns also work.
/// TODO(E4): moves behind the connector trait/registry.
fn resolve_glob(root: &Path, pattern: &str) -> Result<Vec<PathBuf>> {
    // `root.join` silently discards the root for an absolute pattern, which
    // would let a manifest read outside the connector root.
    if Path::new(pattern).is_absolute() {
        bail!("glob pattern must be relative to the connector root, got \"{pattern}\"");
    }
    let absolute = root.join(pattern);
    let pattern_str = absolute
        .to_str()
        .context("glob pattern is not valid UTF-8")?;
    let mut paths: Vec<PathBuf> = glob::glob(pattern_str)
        .context("invalid glob pattern")?
        .collect::<std::result::Result<_, _>>()
        .context("cannot read a glob match")?;
    paths.sort();
    Ok(paths)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn resolve_glob_returns_sorted_matches() {
        let dir = std::env::temp_dir().join(format!("wv-glob-{}", std::process::id()));
        std::fs::create_dir_all(dir.join("in")).unwrap();
        std::fs::write(dir.join("in/b.json"), "{}").unwrap();
        std::fs::write(dir.join("in/a.json"), "{}").unwrap();

        let paths = resolve_glob(&dir, "in/*.json").unwrap();
        let names: Vec<_> = paths
            .iter()
            .map(|p| p.file_name().unwrap().to_str().unwrap())
            .collect();
        assert_eq!(names, ["a.json", "b.json"]);

        std::fs::remove_dir_all(&dir).unwrap();
    }

    #[test]
    fn resolve_glob_rejects_an_absolute_pattern() {
        let err = resolve_glob(Path::new("/tmp"), "/etc/*.conf")
            .unwrap_err()
            .to_string();
        assert!(err.contains("must be relative"), "{err}");
    }

    #[test]
    fn resolve_glob_treats_a_literal_path_as_one_match() {
        let dir = std::env::temp_dir().join(format!("wv-glob-lit-{}", std::process::id()));
        std::fs::create_dir_all(dir.join("in")).unwrap();
        std::fs::write(dir.join("in/x.json"), "{}").unwrap();

        let paths = resolve_glob(&dir, "in/x.json").unwrap();
        assert_eq!(paths.len(), 1);

        std::fs::remove_dir_all(&dir).unwrap();
    }
}
