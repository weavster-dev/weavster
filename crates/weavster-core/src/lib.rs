//! Weavster Core Library
//!
//! This crate provides the core functionality for Weavster:
//! - Configuration parsing and validation
//! - Transform DSL and execution
//! - Connector trait and implementations
//! - Flow orchestration logic
//!
//! # Architecture
//!
//! ```text
//! ┌─────────────┐     ┌─────────────┐     ┌─────────────┐
//! │   Config    │────▶│  Transform  │────▶│  Connector  │
//! │   (YAML)    │     │   Engine    │     │   Output    │
//! └─────────────┘     └─────────────┘     └─────────────┘
//! ```
//!
//! # Example
//!
//! ```rust,ignore
//! use weavster_core::{Config, Flow, Project};
//!
//! let project = Project::load("./weavster.yaml")?;
//! for flow in project.flows() {
//!     println!("Flow: {}", flow.name());
//! }
//! ```

#![deny(unsafe_code)]
#![warn(missing_docs)]
#![warn(clippy::all)]

pub mod config;
pub mod connectors;
pub mod error;
pub mod flow;
pub mod interpreter;

/// The evaluation and assertion engine backend
pub mod testing;

pub mod transforms;

/// WASM runtime for executing compiled transforms
#[cfg(feature = "wasm")]
pub mod wasm;

pub use config::{
    BackoffStrategy, Config, ConfigCache, DynamicJinjaContext, ErrorHandlingConfig, JinjaContext,
    LogLevel, MacroDefinition, OnErrorBehavior, ProfileConfig, ProjectConfig, ResolvedConfig,
    RetryConfig,
};
pub use error::{Error, Result};
pub use flow::Flow;
