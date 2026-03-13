//! WASM Execution Runtime Integration

use anyhow::{Result, anyhow};
use std::path::Path;
use wasmtime::{Config, Engine, Linker, Module, Store};

/// The embedded WASM execution environment that orchestrates compilation/caching/calling
pub struct WasmRuntime {
    engine: Engine,
}

impl WasmRuntime {
    /// Boot up a new isolated WasmRuntime environment
    pub fn new() -> Result<Self> {
        let mut config = Config::new();
        // Since we are compiling as simple standard WASM modules now, we disable the component model
        config.wasm_component_model(false);
        let engine = Engine::new(&config)?;

        Ok(Self { engine })
    }

    /// Load the raw compiled `.wasm` file, write the JSON byte array inside it, and extract the parsed string
    pub fn execute(&self, wasm_path: &Path, input: &[u8]) -> Result<Vec<u8>> {
        let module = Module::from_file(&self.engine, wasm_path)?;
        let mut store = Store::new(&self.engine, ());
        let linker = Linker::new(&self.engine);

        // Execute instantiation
        let instance = linker.instantiate(&mut store, &module)?;

        // Find the exported memory map to safely write the input bytes bounds
        let memory = instance
            .get_memory(&mut store, "memory")
            .ok_or_else(|| anyhow!("Failed to find exported memory in WASM Module"))?;

        // Standard memory configuration limit handles payloads up to 1MB currently (as an MVP safeguard)
        let output_capacity = 1024 * 1024;

        // Find exported transform function: Fn(input_ptr: i32, input_len: i32, output_ptr: i32, output_cap: i32) -> i32
        let transform_fn =
            instance.get_typed_func::<(i32, i32, i32, i32), i32>(&mut store, "transform")?;

        // Assuming pure memory map scaling allocations since it is entirely sandbox agnostic
        let data = memory.data_mut(&mut store);

        // We will statically offset local sandbox RAM to fit:
        // [0..input_len] -> The dynamic JSON input bytes.
        // [input_len..input_len + output_capacity] -> Setup capacity wrapper where output is piped.
        let input_len = input.len();
        let input_ptr = 0;
        let output_ptr = input_len;

        // Verify bounds constraint explicitly
        if input_len + output_capacity > data.len() {
            // Ask WASM to grow memory block frames if needed
            let pages_needed = ((input_len + output_capacity - data.len()) / (64 * 1024)) + 1;
            memory.grow(&mut store, pages_needed as u64)?;
        }

        // Re-borrow the potentially grown memory
        let data = memory.data_mut(&mut store);

        // Inject the bytes manually into offset zero
        data[input_ptr..input_len].copy_from_slice(input);

        // Execute processing!
        let input_ptr_i32 = i32::try_from(input_ptr).map_err(|_| anyhow!("input_ptr overflow"))?;
        let input_len_i32 = i32::try_from(input_len).map_err(|_| anyhow!("input_len overflow"))?;
        let output_ptr_i32 =
            i32::try_from(output_ptr).map_err(|_| anyhow!("output_ptr overflow"))?;
        let output_capacity_i32 =
            i32::try_from(output_capacity).map_err(|_| anyhow!("output_capacity overflow"))?;

        let output_len = transform_fn.call(
            &mut store,
            (
                input_ptr_i32,
                input_len_i32,
                output_ptr_i32,
                output_capacity_i32,
            ),
        )?;

        if output_len < 0 {
            return Err(anyhow!(
                "WASM transform failed with error code: {}",
                output_len
            ));
        }

        let output_len_usize = output_len as usize;
        if output_len_usize > output_capacity {
            return Err(anyhow!(
                "WASM transform returned more data than capacity: {} > {}",
                output_len_usize,
                output_capacity
            ));
        }

        // Pull the output payload back to the host process
        let data = memory.data(&store);
        let end = output_ptr
            .checked_add(output_len_usize)
            .ok_or_else(|| anyhow!("output pointer overflow"))?;
        if end > data.len() {
            return Err(anyhow!(
                "WASM output out of bounds: {} > {}",
                end,
                data.len()
            ));
        }

        let output_bytes = &data[output_ptr..end];

        Ok(output_bytes.to_vec())
    }
}
