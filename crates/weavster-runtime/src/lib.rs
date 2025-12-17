//! Weavster Runtime
//!
//! This crate provides the execution runtime for Weavster flows.
//! It's designed to be minimal for small Docker images.
//!
//! # Features
//!
//! - Job queue management via apalis
//! - Flow execution engine
//! - Connector lifecycle management
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

pub use engine::Runtime;
pub use error::{Error, Result};
