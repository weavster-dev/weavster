//! WASM compiler
//!
//! Compiles generated Rust code to WASM using cargo/rustc.

use std::path::{Path, PathBuf};
use tokio::process::Command;

use crate::error::{Error, Result};
use crate::generator::Generator;
use crate::parser::Parser;

/// Options for the compiler
#[derive(Debug, Clone)]
pub struct CompileOptions {
    /// Output directory for WASM files
    pub output_dir: PathBuf,

    /// Cache directory for intermediate artifacts
    pub cache_dir: PathBuf,

    /// Whether to include debug info
    pub debug: bool,

    /// Optimization level (0-3, 's', 'z')
    pub opt_level: String,

    /// Whether to skip cache and force recompile
    pub force: bool,
}

impl Default for CompileOptions {
    fn default() -> Self {
        Self {
            output_dir: PathBuf::from(".weavster/output"),
            cache_dir: PathBuf::from(".weavster/cache"),
            debug: false,
            opt_level: "s".to_string(), // Optimize for size
            force: false,
        }
    }
}

/// WASM compiler
pub struct Compiler {
    options: CompileOptions,
    parser: Parser,
    generator: Generator,
}

impl Compiler {
    /// Create a new compiler with the given options
    pub fn new(options: CompileOptions) -> Self {
        Self {
            parser: Parser::new("."),
            generator: if options.debug {
                Generator::new().with_debug_comments()
            } else {
                Generator::new()
            },
            options,
        }
    }

    /// Compile a flow YAML file to WASM
    pub async fn compile_flow(&self, flow_path: impl AsRef<Path>) -> Result<CompiledFlow> {
        let flow_path = flow_path.as_ref();
        tracing::info!("Compiling flow: {}", flow_path.display());

        // Parse YAML to IR
        let ir = self.parser.parse_file(flow_path)?;

        // Check cache
        let cache_key = ir.content_hash();
        let cached_wasm = self.options.cache_dir.join(format!("{}.wasm", cache_key));

        if !self.options.force && cached_wasm.exists() {
            tracing::debug!("Using cached WASM: {}", cached_wasm.display());
            let wasm_bytes = std::fs::read(&cached_wasm)?;
            return Ok(CompiledFlow {
                name: ir.name.clone(),
                wasm: wasm_bytes,
                hash: cache_key,
            });
        }

        // Generate Rust code
        let rust_code = self.generator.generate(&ir)?;

        // Write to temp crate
        let temp_crate = self.create_temp_crate(&ir.name, &rust_code).await?;

        // Compile to WASM
        let wasm_bytes = self.compile_to_wasm(&temp_crate, &ir.name).await?;

        // Cache the result
        std::fs::create_dir_all(&self.options.cache_dir)?;
        std::fs::write(&cached_wasm, &wasm_bytes)?;

        // Also save generated Rust for debugging
        if self.options.debug {
            let rust_path = self.options.cache_dir.join(format!("{}.rs", ir.name));
            std::fs::write(&rust_path, &rust_code)?;
            tracing::debug!("Saved generated Rust: {}", rust_path.display());
        }

        Ok(CompiledFlow {
            name: ir.name,
            wasm: wasm_bytes,
            hash: cache_key,
        })
    }

    /// Compile all flows in a directory
    pub async fn compile_all(&self, flows_dir: impl AsRef<Path>) -> Result<Vec<CompiledFlow>> {
        let flows_dir = flows_dir.as_ref();
        let mut results = Vec::new();

        for entry in walkdir::WalkDir::new(flows_dir)
            .into_iter()
            .filter_map(|e| e.ok())
            .filter(|e| {
                e.path()
                    .extension()
                    .is_some_and(|ext| ext == "yaml" || ext == "yml")
            })
        {
            let compiled = self.compile_flow(entry.path()).await?;
            results.push(compiled);
        }

        Ok(results)
    }

    async fn create_temp_crate(&self, name: &str, rust_code: &str) -> Result<PathBuf> {
        let temp_dir = self.options.cache_dir.join(format!("build_{}", name));
        std::fs::create_dir_all(&temp_dir)?;

        // Create Cargo.toml
        let cargo_toml = format!(
            r#"[package]
name = "{name}"
version = "0.1.0"
edition = "2021"

[lib]
crate-type = ["cdylib"]

[dependencies]
serde_json = {{ version = "1.0", default-features = false, features = ["alloc"] }}

[profile.release]
lto = true
opt-level = "{opt}"
codegen-units = 1
panic = "abort"
strip = true
"#,
            name = name,
            opt = self.options.opt_level,
        );

        std::fs::write(temp_dir.join("Cargo.toml"), cargo_toml)?;

        // Create src directory and lib.rs
        let src_dir = temp_dir.join("src");
        std::fs::create_dir_all(&src_dir)?;
        std::fs::write(src_dir.join("lib.rs"), rust_code)?;

        Ok(temp_dir)
    }

    async fn compile_to_wasm(&self, crate_dir: &Path, name: &str) -> Result<Vec<u8>> {
        // Check for wasm32-wasi target
        self.ensure_wasm_target().await?;

        tracing::debug!("Running cargo build for WASM target");

        let output = Command::new("cargo")
            .current_dir(crate_dir)
            .args(["build", "--release", "--target", "wasm32-wasi"])
            .output()
            .await
            .map_err(|e| Error::ToolchainError {
                message: format!("Failed to run cargo: {}", e),
            })?;

        if !output.status.success() {
            let stderr = String::from_utf8_lossy(&output.stderr);
            return Err(Error::CompilationError {
                message: "Cargo build failed".to_string(),
                stderr: Some(stderr.to_string()),
            });
        }

        // Read the compiled WASM
        let wasm_path = crate_dir
            .join("target")
            .join("wasm32-wasi")
            .join("release")
            .join(format!("{}.wasm", name));

        let wasm_bytes = std::fs::read(&wasm_path).map_err(|e| Error::CompilationError {
            message: format!("Failed to read compiled WASM: {}", e),
            stderr: None,
        })?;

        tracing::info!("Compiled WASM: {} bytes", wasm_bytes.len());

        Ok(wasm_bytes)
    }

    async fn ensure_wasm_target(&self) -> Result<()> {
        let output = Command::new("rustup")
            .args(["target", "list", "--installed"])
            .output()
            .await
            .map_err(|e| Error::ToolchainError {
                message: format!("Failed to run rustup: {}", e),
            })?;

        let installed = String::from_utf8_lossy(&output.stdout);

        if !installed.contains("wasm32-wasi") {
            tracing::info!("Installing wasm32-wasi target...");

            let install_output = Command::new("rustup")
                .args(["target", "add", "wasm32-wasi"])
                .output()
                .await
                .map_err(|e| Error::ToolchainError {
                    message: format!("Failed to install wasm32-wasi target: {}", e),
                })?;

            if !install_output.status.success() {
                return Err(Error::ToolchainError {
                    message: "Failed to install wasm32-wasi target".to_string(),
                });
            }
        }

        Ok(())
    }
}

/// A compiled flow ready for execution or packaging
#[derive(Debug)]
pub struct CompiledFlow {
    /// Flow name
    pub name: String,

    /// Compiled WASM bytes
    pub wasm: Vec<u8>,

    /// Content hash (for cache validation)
    pub hash: String,
}

impl CompiledFlow {
    /// Save the WASM to a file
    pub fn save(&self, path: impl AsRef<Path>) -> Result<()> {
        std::fs::write(path.as_ref(), &self.wasm)?;
        Ok(())
    }

    /// Get the WASM size in bytes
    pub fn size(&self) -> usize {
        self.wasm.len()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_compile_options_default() {
        let opts = CompileOptions::default();
        assert!(!opts.debug);
        assert_eq!(opts.opt_level, "s");
    }
}
