//! Weavster Code Generation
//!
//! This crate handles the YAML → Rust → WASM compilation pipeline.
//!
//! # Pipeline Overview
//!
//! ```text
//! ┌─────────┐     ┌─────────┐     ┌─────────┐     ┌─────────┐
//! │  YAML   │────▶│   IR    │────▶│  Rust   │────▶│  WASM   │
//! │ Config  │     │ (Parse) │     │ (Gen)   │     │(Compile)│
//! └─────────┘     └─────────┘     └─────────┘     └─────────┘
//! ```
//!
//! # Example
//!
//! ```rust,ignore
//! use weavster_codegen::{Compiler, CompileOptions};
//!
//! let compiler = Compiler::new(CompileOptions::default());
//! let wasm_bytes = compiler.compile_flow("flows/my_flow.yaml").await?;
//! ```

#![forbid(unsafe_code)]
#![warn(missing_docs)]

pub mod compiler;
pub mod error;
pub mod generator;
pub mod ir;
pub mod parser;
pub mod transforms;

pub use compiler::{CompileOptions, Compiler};
pub use error::{Error, Result};
pub use ir::FlowIR;
