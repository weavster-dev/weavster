//! Weavster Runtime
//!
//! This crate provides the execution runtime for Weavster flows.
//!
//!
//! # Features
//!
//! - WASM-backed flow execution engine
//! - File connector input/output runtime
//! - SQLite and Postgres state store implementations
//!
//! # Usage
//!
//! ```rust,ignore
//! use weavster_runtime::Runtime;
//!
//! let runtime = Runtime::new(config).await?;
//! runtime.start().await?;
//! ```

#![forbid(unsafe_code)]
#![warn(missing_docs)]

pub mod engine;
pub mod error;
pub mod jobs;
/// Database models
pub mod models;
/// State store implementations
pub mod state;

/// Re-export WASM runtime from core
pub use weavster_core::wasm;

pub use engine::Runtime;
pub use error::{Error, Result};
