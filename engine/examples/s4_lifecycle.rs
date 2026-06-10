//! S4 spike: instance lifecycle for Javy flow modules.
//!
//! A Javy module is a WASI *command* — `_start` runs main exactly once — so
//! "re-init a pooled instance" cannot mean re-running an existing instance.
//! The real comparison is where the cost sits:
//!   - `Module::new` (JIT compile of the 2.5 MB module): once.
//!   - fresh `Store` + instantiate + `_start` per document: per call.
//!
//! Run: cargo run --release --example s4_lifecycle -- <path/to/order.wasm>

use std::time::Instant;
use wasmtime::{Engine, Linker, Module, Store};
use wasmtime_wasi::p2::WasiCtxBuilder;
use wasmtime_wasi::p2::pipe::{MemoryInputPipe, MemoryOutputPipe};
use wasmtime_wasi::preview1::{self, WasiP1Ctx};

fn run_one(engine: &Engine, module: &Module, linker: &Linker<WasiP1Ctx>, input: &str) -> String {
    let stdout = MemoryOutputPipe::new(1 << 20);
    let wasi = WasiCtxBuilder::new()
        .stdin(MemoryInputPipe::new(input.to_string()))
        .stdout(stdout.clone())
        .inherit_stderr()
        .build_p1();
    let mut store = Store::new(engine, wasi);
    let instance = linker.instantiate(&mut store, module).expect("instantiate");
    let start = instance
        .get_typed_func::<(), ()>(&mut store, "_start")
        .expect("_start");
    start.call(&mut store, ()).expect("run");
    drop(store);
    String::from_utf8(stdout.contents().to_vec()).expect("utf8")
}

fn main() {
    let wasm_path = std::env::args()
        .nth(1)
        .expect("usage: s4_lifecycle <flow.wasm>");
    let engine = Engine::default();

    let t = Instant::now();
    let module = Module::from_file(&engine, &wasm_path).expect("compile");
    let compile_ms = t.elapsed().as_millis();

    let mut linker: Linker<WasiP1Ctx> = Linker::new(&engine);
    preview1::add_to_linker_sync(&mut linker, |ctx| ctx).expect("link wasi");

    let envelope = r#"{"in":"json","out":"json","payload":"{\"id\":\"a1\",\"first\":\"Ada\",\"last\":\"Lovelace\",\"status\":\"new\"}"}"#;

    // Warm-up + correctness: N runs must produce byte-identical output.
    let first = run_one(&engine, &module, &linker, envelope);
    println!("first output: {}", &first[..first.len().min(120)]);

    let n = 50;
    let t = Instant::now();
    let mut all_same = true;
    for _ in 0..n {
        let out = run_one(&engine, &module, &linker, envelope);
        all_same &= out == first;
    }
    let per_doc_us = t.elapsed().as_micros() / n;

    println!("module compile: {compile_ms} ms (once)");
    println!("fresh store+instantiate+run: {per_doc_us} us/doc over {n} docs");
    println!("stable output across {n} fresh instances: {all_same}");
}
