//! Error types for weavster-core

use thiserror::Error;

/// Result type alias for weavster-core operations
pub type Result<T> = std::result::Result<T, Error>;

/// Errors that can occur in weavster-core
#[derive(Error, Debug)]
pub enum Error {
    /// Configuration file could not be found
    #[error("configuration file not found: {path}")]
    ConfigNotFound {
        /// Path that was searched
        path: String,
    },

    /// Failed to parse YAML configuration
    #[error("failed to parse configuration: {0}")]
    ConfigParse(#[from] serde_yaml::Error),

    /// Invalid configuration value
    #[error("invalid configuration: {message}")]
    ConfigInvalid {
        /// Description of what's invalid
        message: String,
    },

    /// Flow definition error
    #[error("invalid flow '{flow_name}': {message}")]
    InvalidFlow {
        /// Name of the flow with the error
        flow_name: String,
        /// Description of the error
        message: String,
    },

    /// Transform execution error
    #[error("transform error in '{transform}': {message}")]
    TransformError {
        /// Name or type of the transform
        transform: String,
        /// Description of the error
        message: String,
    },

    /// Connector error
    #[error("connector '{connector}' error: {message}")]
    ConnectorError {
        /// Name of the connector
        connector: String,
        /// Description of the error
        message: String,
    },

    /// Template rendering error
    #[error("template error: {0}")]
    TemplateError(#[from] minijinja::Error),

    /// Macro expansion error
    #[error("macro error in '{macro_name}': {message}")]
    MacroError {
        /// Name of the macro
        macro_name: String,
        /// Description of the error
        message: String,
        /// Source file path
        file: Option<std::path::PathBuf>,
        /// Line number in the source file
        line: Option<usize>,
    },

    /// Profile resolution error
    #[error("profile '{profile_name}': {message}")]
    ProfileError {
        /// Name of the profile
        profile_name: String,
        /// Description of the error
        message: String,
    },

    /// IO error
    #[error("IO error: {0}")]
    Io(#[from] std::io::Error),

    /// JSON serialization/deserialization error
    #[error("JSON error: {0}")]
    Json(#[from] serde_json::Error),
}
