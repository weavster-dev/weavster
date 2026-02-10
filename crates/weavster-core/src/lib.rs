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

#![forbid(unsafe_code)]
#![warn(missing_docs)]
#![warn(clippy::all)]

pub mod config;
pub mod connectors;
pub mod error;
pub mod flow;
pub mod interpreter;
pub mod transforms;

pub use config::{Config, ProjectConfig};
pub use error::{Error, Result};
pub use flow::Flow;
