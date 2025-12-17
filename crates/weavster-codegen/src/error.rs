//! Error types for code generation

use thiserror::Error;

/// Result type for codegen operations
pub type Result<T> = std::result::Result<T, Error>;

/// Errors that can occur during code generation
#[derive(Error, Debug)]
pub enum Error {
    /// Failed to parse YAML configuration
    #[error("failed to parse flow configuration: {0}")]
    ParseError(#[from] serde_yaml::Error),

    /// Invalid transform configuration
    #[error("invalid transform in flow '{flow}': {message}")]
    InvalidTransform {
        /// Flow name
        flow: String,
        /// Error description
        message: String,
    },

    /// Failed to generate Rust code
    #[error("code generation failed: {0}")]
    GenerationError(String),

    /// Failed to compile to WASM
    #[error("WASM compilation failed: {message}")]
    CompilationError {
        /// Error message
        message: String,
        /// Compiler stderr output
        stderr: Option<String>,
    },

    /// Artifact not found (translation table, regex pattern file)
    #[error("artifact not found: {path}")]
    ArtifactNotFound {
        /// Path to missing artifact
        path: String,
    },

    /// Invalid regex pattern
    #[error("invalid regex pattern '{pattern}': {message}")]
    InvalidRegex {
        /// The pattern that failed
        pattern: String,
        /// Error message
        message: String,
    },

    /// Invalid Jinja template
    #[error("invalid template: {0}")]
    InvalidTemplate(#[from] minijinja::Error),

    /// IO error
    #[error("IO error: {0}")]
    Io(#[from] std::io::Error),

    /// Cache error
    #[error("cache error: {0}")]
    CacheError(String),

    /// Rust toolchain not found
    #[error("Rust toolchain error: {message}. Ensure rustup and wasm32-wasi target are installed.")]
    ToolchainError {
        /// Error message
        message: String,
    },
}
