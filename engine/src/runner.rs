//! Per-pipeline run loop (Engine Plan E3 slice 3, E4 connectors).
//!
//! Each pipeline is `source → transform → sink` behind a FIFO queue with
//! concurrency 1 (documents stay in input order); pipelines run concurrently
//! with each other as tokio tasks. I/O is async (the connector traits); the
//! transform is synchronous and runs in `spawn_blocking`. Error scoping per
//! RFC 0002/0003: startup errors abort the run; per-document failures fail a
//! bounded run and would log-and-move-on on a live stream (every source this
//! phase is bounded — files).

use crate::connector::{Sink, Source};
use crate::host::{FlowModule, Host, InputEnvelope};
use crate::log;
use crate::manifest::Manifest;
use crate::registry;
use anyhow::{Context, Result, bail};
use std::collections::HashMap;
use std::path::Path;
use std::sync::Arc;
use tokio::task::JoinSet;

pub struct RunReport {
    /// Pipeline name → error message, for pipelines that failed.
    pub failures: Vec<(String, String)>,
    pub documents: usize,
}

/// Load every flow the manifest references (deduplicated), then run all
/// pipelines concurrently. The connector root is the artifact directory.
pub async fn run(artifact_dir: &Path, manifest: &Manifest) -> Result<RunReport> {
    let host = Host::new()?;
    let mut flows: HashMap<String, Arc<FlowModule>> = HashMap::new();

    // Startup, in declaration order: build each pipeline's connectors (which
    // validates the connector type and opens the source) and load its flow
    // module. Any failure here aborts the whole run before a document moves.
    let mut plans = Vec::with_capacity(manifest.pipelines.len());
    for pipeline in &manifest.pipelines {
        let source = registry::build_source(artifact_dir, &pipeline.source)
            .with_context(|| format!("pipeline \"{}\" source", pipeline.name))?;
        let sink = registry::build_sink(artifact_dir, &pipeline.sink)
            .with_context(|| format!("pipeline \"{}\" sink", pipeline.name))?;
        if !flows.contains_key(&pipeline.flow) {
            let module = host
                .load_flow(artifact_dir, &pipeline.flow)
                .with_context(|| format!("pipeline \"{}\"", pipeline.name))?;
            flows.insert(pipeline.flow.clone(), Arc::new(module));
        }
        plans.push(PipelinePlan {
            name: pipeline.name.clone(),
            in_format: pipeline.source.format.as_str().into(),
            out_format: pipeline.sink.format.as_str().into(),
            source,
            sink,
            flow: Arc::clone(&flows[&pipeline.flow]),
        });
    }

    // Spawn one task per pipeline; tasks own their connectors and share the
    // flow module behind an Arc. The task id → name map lets a panicking
    // pipeline be recorded as a failure (with its name) without aborting the
    // others — pipelines stay isolated, as on E3's scoped threads.
    let mut set: JoinSet<Result<usize>> = JoinSet::new();
    let mut names: HashMap<tokio::task::Id, String> = HashMap::new();
    for plan in plans {
        let name = plan.name.clone();
        let handle = set.spawn(run_pipeline(plan));
        names.insert(handle.id(), name);
    }

    let mut failures = Vec::new();
    let mut documents = 0;
    while let Some(joined) = set.join_next_with_id().await {
        match joined {
            Ok((_, Ok(count))) => documents += count,
            Ok((id, Err(err))) => failures.push((names[&id].clone(), format!("{err:#}"))),
            Err(join_err) => {
                let name = names.get(&join_err.id()).cloned().unwrap_or_default();
                failures.push((name, "pipeline task panicked".into()));
            }
        }
    }
    Ok(RunReport {
        failures,
        documents,
    })
}

/// Everything one pipeline task owns: its name and the source/sink formats
/// (the only manifest fields the loop needs), its built connectors, and a
/// handle to the shared flow module. The formats are `Arc<str>` so each
/// document's `spawn_blocking` clone is one atomic bump, not a fresh alloc.
struct PipelinePlan {
    name: String,
    in_format: Arc<str>,
    out_format: Arc<str>,
    source: Box<dyn Source>,
    sink: Box<dyn Sink>,
    flow: Arc<FlowModule>,
}

/// One pipeline: pull each document from the source in order, run it through
/// the flow, write the result to the sink. Returns the document count.
async fn run_pipeline(plan: PipelinePlan) -> Result<usize> {
    let PipelinePlan {
        name,
        in_format,
        out_format,
        mut source,
        mut sink,
        flow,
    } = plan;

    let mut documents = 0;
    while let Some(doc) = source.next().await? {
        documents += 1;

        // The transform is synchronous and CPU-bound; run it off the async
        // worker so it never blocks other pipelines' I/O.
        let result = {
            let flow = Arc::clone(&flow);
            let in_format = Arc::clone(&in_format);
            let out_format = Arc::clone(&out_format);
            let payload = doc.payload;
            tokio::task::spawn_blocking(move || {
                flow.run(&InputEnvelope {
                    r#in: &in_format,
                    out: &out_format,
                    payload: &payload,
                })
            })
            .await
            .context("transform task panicked")?
            .with_context(|| format!("document {documents} ({})", doc.origin))?
        };

        if !result.ok {
            let error = result.error.as_ref();
            let stage = error.map_or("unknown", |e| e.stage.as_str());
            let error_type = error
                .and_then(|e| e.error_type.as_deref())
                .unwrap_or("unknown");
            let message = error
                .and_then(|e| e.message.as_deref())
                .unwrap_or("(no message)");
            log::error(&name, documents, stage, error_type, message);
            // Every source this phase is bounded (files), so a poison document
            // fails the run. A live stream would log-and-move-on here instead.
            bail!("document {documents}: {stage}: {message}");
        }

        let output = result
            .payload
            .context("ok envelope is missing its payload")?;
        sink.write(&output).await?;
        log::done(&name, documents);
    }
    Ok(documents)
}
