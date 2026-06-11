//! wasmtime host over the Javy stdin/stdout ABI (Engine Plan E3 slice 2).
//!
//! Lifecycle per the S4 spike: JIT-compile each flow `Module` once at startup
//! (~220 ms for a 2.5 MB Javy module), then a fresh `Store` + instance per
//! document (~1.3 ms) — a Javy module is a WASI command whose `_start` runs
//! exactly once, and a fresh store gives perfect isolation between documents.
//!
//! Resource limits (TODO(config) defaults): a memory cap per store and an
//! epoch-based wall-clock deadline so a runaway `_ts` (infinite loop) is
//! interrupted instead of hanging the pipeline.

use anyhow::{Context, Result, bail};
use serde::{Deserialize, Serialize};
use std::path::Path;
use std::sync::Arc;
use std::time::Duration;
use wasmtime::{Config, Engine, Linker, Module, Store, StoreLimits, StoreLimitsBuilder};
use wasmtime_wasi::p2::WasiCtxBuilder;
use wasmtime_wasi::p2::pipe::{MemoryInputPipe, MemoryOutputPipe};
use wasmtime_wasi::preview1::{self, WasiP1Ctx};

// TODO(config): per-flow tuning. Defaults per RFC 0003 ("defaults now").
const MEMORY_CAP_BYTES: usize = 256 * 1024 * 1024;
const WALL_CLOCK_LIMIT: Duration = Duration::from_secs(10);
const EPOCH_TICK: Duration = Duration::from_millis(100);
const STDOUT_CAP_BYTES: usize = 64 * 1024 * 1024;

/// The input envelope the host writes to a flow module's stdin.
#[derive(Serialize)]
pub struct InputEnvelope<'a> {
    pub r#in: &'a str,
    pub out: &'a str,
    pub payload: &'a str,
}

/// The result envelope a flow module writes to stdout.
#[derive(Debug, Deserialize)]
pub struct ResultEnvelope {
    pub ok: bool,
    pub payload: Option<String>,
    pub error: Option<EnvelopeError>,
}

#[derive(Debug, Deserialize)]
pub struct EnvelopeError {
    pub stage: String,
    #[serde(rename = "type")]
    pub error_type: Option<String>,
    pub message: Option<String>,
}

struct HostState {
    wasi: WasiP1Ctx,
    limits: StoreLimits,
}

/// A compiled flow module, reusable across documents and threads.
pub struct FlowModule {
    engine: Arc<Engine>,
    module: Module,
    linker: Arc<Linker<HostState>>,
}

/// The shared wasmtime engine. One per process; flows compile against it.
pub struct Host {
    engine: Arc<Engine>,
    linker: Arc<Linker<HostState>>,
}

impl Host {
    pub fn new() -> Result<Self> {
        let mut config = Config::new();
        config.epoch_interruption(true);
        let engine = Arc::new(Engine::new(&config)?);

        let mut linker: Linker<HostState> = Linker::new(&engine);
        preview1::add_to_linker_sync(&mut linker, |state| &mut state.wasi)
            .context("link WASI preview1")?;

        // One background thread advances the epoch for every store on this
        // engine; each store sets its own deadline in ticks.
        let weak = Arc::downgrade(&engine);
        std::thread::spawn(move || {
            while let Some(engine) = weak.upgrade() {
                engine.increment_epoch();
                drop(engine);
                std::thread::sleep(EPOCH_TICK);
            }
        });

        Ok(Self {
            engine,
            linker: Arc::new(linker),
        })
    }

    /// JIT-compile `flows/<flow>.wasm` from the artifact (once per flow).
    pub fn load_flow(&self, artifact_dir: &Path, flow: &str) -> Result<FlowModule> {
        let path = artifact_dir.join("flows").join(format!("{flow}.wasm"));
        let module = Module::from_file(&self.engine, &path)
            .with_context(|| format!("cannot load flow module {}", path.display()))?;
        Ok(FlowModule {
            engine: Arc::clone(&self.engine),
            module,
            linker: Arc::clone(&self.linker),
        })
    }
}

impl FlowModule {
    /// Run one document through the flow: fresh store, write the input
    /// envelope to stdin, run `_start`, parse the result envelope from stdout.
    pub fn run(&self, input: &InputEnvelope<'_>) -> Result<ResultEnvelope> {
        let stdin = serde_json::to_string(input).context("encode input envelope")?;
        let stdout = MemoryOutputPipe::new(STDOUT_CAP_BYTES);

        let wasi = WasiCtxBuilder::new()
            .stdin(MemoryInputPipe::new(stdin))
            .stdout(stdout.clone())
            .inherit_stderr()
            .build_p1();
        let limits = StoreLimitsBuilder::new()
            .memory_size(MEMORY_CAP_BYTES)
            .build();

        let mut store = Store::new(&self.engine, HostState { wasi, limits });
        store.limiter(|state| &mut state.limits);
        let deadline_ticks = WALL_CLOCK_LIMIT.as_millis() / EPOCH_TICK.as_millis();
        store.set_epoch_deadline(deadline_ticks as u64);

        let instance = self
            .linker
            .instantiate(&mut store, &self.module)
            .context("instantiate flow module")?;
        let start = instance
            .get_typed_func::<(), ()>(&mut store, "_start")
            .context("flow module has no _start export")?;
        start
            .call(&mut store, ())
            .context("flow module trapped (memory/time limit or internal error)")?;
        drop(store);

        let bytes = stdout.contents();
        if bytes.is_empty() {
            bail!("flow module produced no output");
        }
        serde_json::from_slice(&bytes).context("flow module output is not a result envelope")
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // Host tests that need a real .wasm artifact live in tests/engine.rs (they
    // depend on `weavster compile` output). Here: envelope shapes only.

    #[test]
    fn input_envelope_serializes_with_contract_field_names() {
        let e = InputEnvelope {
            r#in: "json",
            out: "xml",
            payload: "{}",
        };
        let json = serde_json::to_string(&e).unwrap();
        assert_eq!(json, r#"{"in":"json","out":"xml","payload":"{}"}"#);
    }

    #[test]
    fn result_envelope_parses_ok_and_error_shapes() {
        let ok: ResultEnvelope = serde_json::from_str(r#"{"ok":true,"payload":"x"}"#).unwrap();
        assert!(ok.ok);
        assert_eq!(ok.payload.as_deref(), Some("x"));

        let err: ResultEnvelope = serde_json::from_str(
            r#"{"ok":false,"error":{"stage":"parse","type":"JsonParseError","message":"bad"}}"#,
        )
        .unwrap();
        assert!(!err.ok);
        let detail = err.error.unwrap();
        assert_eq!(detail.stage, "parse");
        assert_eq!(detail.message.as_deref(), Some("bad"));
    }
}
