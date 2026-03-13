# WASM Compilation Pipeline: Codegen & Runtime

## Overview

Implement the WASM compilation pipeline that compiles flow configurations to WASM modules and executes them. This is the core technical proof for the MVP: demonstrating that YAML transforms can be compiled to WASM and executed efficiently.

**Spec References:**

- spec:3afb66cd-a148-48b8-8ce9-0bf5860c562d/e13dad9c-eda8-46d1-be4f-09ce88c29a23 - Architectural Approach (WASM Strategy), Component Architecture (WASM Runtime)
- spec:3afb66cd-a148-48b8-8ce9-0bf5860c562d/ae5101d9-bf0f-4073-b8a0-1576e4feea30 - Epic Brief (WASM compilation is essential)

## Scope

**In Scope:**

- Enhance file:crates/weavster-codegen/ to support MVP requirements:
  - Switch target from `wasm32-wasi` to `wasm32-wasip2`
  - Switch from `#![no_std]` to `std` in generated code
  - Implement simpler ABI (alloc-style, not pointer/capacity)
  - Add `add_fields` transform support (currently missing)
  - Implement `filter` transform as boolean matcher (currently incomplete)
  - Support structured filter matchers (equals, contains, matches, and/or/not, null checks)
  - Update temp crate dependencies (serde_json + regex + minijinja + once_cell + phf)
  - Per-flow WASM compilation (one module per flow)
  - Filesystem caching in `.weavster/cache/`
  - Hybrid compilation: explicit `weavster compile` + auto-compile fallback
- Implement WASM runtime integration:
  - Initialize `wasmtime` runtime
  - Load compiled WASM modules from cache
  - Execute transforms with locked-down WASI context (no FS/env/net)
  - Handle compilation failures (fail fast with detailed errors)
- Add `weavster compile` CLI command

**Out of Scope:**

- Custom Rust transforms (post-MVP)
- Interpreter fallback (WASM-only for MVP)
- Pre-compiled WASM modules (runtime compilation only)
- WASM optimization passes (focus on correctness first)

## Key Architectural Decisions

1. **WASM Target:** `wasm32-wasip2` with `std`
  - More reliable than `no_std` for MVP
  - Easier dependency management (regex, minijinja work out of box)
  - Still sandboxed via WASI context restrictions
2. **WASM ABI:** Simpler alloc-style interface
  ```rust
   // Generated WASM exports this function
   #[no_mangle]
   pub extern "C" fn transform(input_ptr: *const u8, input_len: usize) -> *const u8 {
       // Allocate output, return pointer
       // Host reads length from first 4 bytes
   }
  ```
  - Easier to debug than pointer/capacity ABI
  - Better for chaining transforms
  - Simpler runtime integration
3. **Filter Implementation:** Boolean matcher transform
  - Filter returns boolean (true = keep, false = drop)
  - Structured matchers only in MVP:
    ```yaml
    filter:
      and:
        - field: status
          equals: "active"
        - field: email
          contains: "@example.com"
        - or:
            - field: age
              gte: 18
            - field: verified
              equals: true
    ```
  - No expression strings (e.g., `when: "status == 'active'"`) in MVP
4. **Compilation Strategy:** Hybrid
  - Explicit: `weavster compile` compiles all flows
  - Auto-compile: `weavster run` compiles if needed (lazy fallback)
  - Cache invalidation: recompile if flow YAML changes (content hash)
5. **Caching:** Filesystem-based
  - Cache location: `.weavster/cache/<flow_name>_<content_hash>.wasm`
  - Simple, inspectable, no database needed
  - Gitignored by default
6. **Security:** Locked-down WASI context
  - No filesystem access
  - No environment variables
  - No network access
  - Monotonic clock only (no wall clock)
  - No argv/stdin/stdout

## Implementation Details

### Codegen Changes

**Add `add_fields` Transform:**

```rust
// In <traycer-file absPath="/Users/greghunt/code/weavster-dev/weavster/crates/weavster-codegen/src/ir.rs">crates/weavster-codegen/src/ir.rs</traycer-file>
pub enum Transform {
    Map { mappings: Vec<FieldMapping> },
    Drop { fields: Vec<String> },
    AddFields { fields: HashMap<String, Value> }, // NEW
    Filter { condition: FilterCondition },
    // ...
}
```

**Implement Filter Matcher:**

```rust
pub enum FilterCondition {
    Equals { field: String, value: Value },
    Contains { field: String, value: String },
    Matches { field: String, pattern: String },
    Null { field: String },
    And { conditions: Vec<FilterCondition> },
    Or { conditions: Vec<FilterCondition> },
    Not { condition: Box<FilterCondition> },
}
```

**Update Temp Crate Cargo.toml:**

```toml
[dependencies]
serde_json = "1.0"
regex = "1.10"
minijinja = "2.0"
once_cell = "1.19"
phf = "0.11"
```

**Switch to `wasm32-wasip2`:**

```rust
// In <traycer-file absPath="/Users/greghunt/code/weavster-dev/weavster/crates/weavster-codegen/src/compiler.rs">crates/weavster-codegen/src/compiler.rs</traycer-file>
let output = Command::new("cargo")
    .args(&["build", "--release", "--target", "wasm32-wasip2"])
    .current_dir(&temp_dir)
    .output()?;
```

### WASM Runtime Integration

```rust
// Pseudocode for runtime
use wasmtime::*;

pub struct WasmRuntime {
    engine: Engine,
    linker: Linker<()>,
}

impl WasmRuntime {
    pub fn new() -> Result<Self> {
        let mut config = Config::new();
        config.wasm_component_model(true);

        let engine = Engine::new(&config)?;
        let mut linker = Linker::new(&engine);

        // Configure locked-down WASI
        wasmtime_wasi::add_to_linker_sync(&mut linker)?;

        Ok(Self { engine, linker })
    }

    pub fn execute(&self, wasm_path: &Path, input: &[u8]) -> Result<Vec<u8>> {
        let module = Module::from_file(&self.engine, wasm_path)?;
        let mut store = Store::new(&self.engine, ());

        // Instantiate with locked-down WASI
        let wasi = WasiCtxBuilder::new()
            .inherit_stdio()
            .build();
        store.data_mut().wasi = wasi;

        let instance = self.linker.instantiate(&mut store, &module)?;

        // Call transform function
        let transform = instance.get_typed_func::<(u32, u32), u32>(&mut store, "transform")?;

        // ... execute and return result
    }
}
```

## Acceptance Criteria

- [ ] Codegen compiles flows to `wasm32-wasip2` target
- [ ] Generated code uses `std` (not `#![no_std]`)
- [ ] `add_fields` transform is implemented and tested
- [ ] `filter` transform supports structured matchers (equals, contains, matches, and/or/not, null)
- [ ] Temp crate includes all standard dependencies
- [ ] WASM modules are cached in `.weavster/cache/`
- [ ] Cache invalidation works (recompile on YAML change)
- [ ] `weavster compile` command compiles all flows
- [ ] Auto-compile works when running flows
- [ ] WASM runtime executes transforms correctly
- [ ] WASI context is locked down (no FS/env/net access)
- [ ] Compilation failures show detailed error messages
- [ ] Unit tests for codegen (IR parsing, code generation)
- [ ] Integration tests for compilation (YAML → WASM)
- [ ] Integration tests for execution (WASM → output)

## Testing Strategy

**Unit Tests:**

- IR parsing for all transform types
- Code generation for each transform
- Filter matcher code generation
- Cache key generation (content hash)

**Integration Tests:**

- Compile simple flow with map transform
- Compile flow with add_fields transform
- Compile flow with filter transform
- Execute compiled WASM with sample data
- Verify cache hit/miss behavior
- Test compilation failure scenarios

**Security Tests:**

- Verify WASM cannot access filesystem
- Verify WASM cannot access environment variables
- Verify WASM cannot make network calls

## Dependencies

- **Depends on:** Config Layer (Ticket 1) - for loading flow configs
- **Depends on:** Validation Layer (Ticket 2) - for validating before compilation

## Estimated Effort

4-5 days (Phase 2 of implementation plan)

## Risk Mitigation

**High Risk:** WASM compilation complexity

- **Mitigation:** Start with simplest transform (map), then add others incrementally
- **Fallback:** If compilation issues arise, simplify codegen (fewer transform types)

**Medium Risk:** WASI compatibility issues

- **Mitigation:** Test on multiple platforms (Linux, macOS, Windows)
- **Fallback:** Document platform-specific issues, focus on primary platform

&nbsp;
