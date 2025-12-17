//! Runtime error types

use anyhow;

/// Result type for runtime operations
pub type Result<T> = anyhow::Result<T>;

/// Runtime error (re-export anyhow for application-level errors)
pub type Error = anyhow::Error;
