//! Configuration parsing and validation
//!
//! This module handles loading and validating Weavster configuration files.
//!
//! # Configuration Files
//!
//! - `weavster.yaml` - Project root configuration
//! - `flows/*.yaml` - Individual flow definitions
//! - `profiles.yaml` - Environment-specific overrides

use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::path::Path;

use crate::error::{Error, Result};

/// Root project configuration from `weavster.yaml`
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ProjectConfig {
    /// Project name
    pub name: String,

    /// Project version
    #[serde(default = "default_version")]
    pub version: String,

    /// Runtime configuration
    #[serde(default)]
    pub runtime: RuntimeConfig,

    /// Global variables available in Jinja templates
    #[serde(default)]
    pub vars: HashMap<String, serde_yaml::Value>,
}

fn default_version() -> String {
    "0.1.0".to_string()
}

/// Runtime configuration
#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct RuntimeConfig {
    /// Runtime mode: local or remote
    #[serde(default)]
    pub mode: RuntimeMode,

    /// Local runtime settings
    #[serde(default)]
    pub local: LocalConfig,

    /// Remote runtime settings
    #[serde(default)]
    pub remote: RemoteConfig,
}

/// Runtime mode
#[derive(Debug, Clone, Copy, Serialize, Deserialize, Default, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
pub enum RuntimeMode {
    /// Local mode with embedded PostgreSQL
    #[default]
    Local,
    /// Remote mode connecting to external services
    Remote,
}

/// Local runtime configuration
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LocalConfig {
    /// Directory for local data (embedded Postgres, caches)
    #[serde(default = "default_data_dir")]
    pub data_dir: String,

    /// Port for embedded PostgreSQL
    #[serde(default = "default_pg_port")]
    pub port: u16,
}

impl Default for LocalConfig {
    fn default() -> Self {
        Self {
            data_dir: default_data_dir(),
            port: default_pg_port(),
        }
    }
}

fn default_data_dir() -> String {
    ".weavster/data".to_string()
}

fn default_pg_port() -> u16 {
    5433 // Avoid conflict with system Postgres on 5432
}

/// Remote runtime configuration
#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct RemoteConfig {
    /// PostgreSQL connection URL
    pub postgres_url: Option<String>,

    /// Redis connection URL (for distributed mode)
    pub redis_url: Option<String>,
}

/// Main configuration container
#[derive(Debug, Clone)]
pub struct Config {
    /// Project configuration
    pub project: ProjectConfig,

    /// Base path of the project
    pub base_path: std::path::PathBuf,
}

impl Config {
    /// Load configuration from a directory
    ///
    /// # Arguments
    ///
    /// * `path` - Path to the project directory or weavster.yaml file
    ///
    /// # Example
    ///
    /// ```rust,ignore
    /// let config = Config::load("./my-project")?;
    /// println!("Project: {}", config.project.name);
    /// ```
    pub fn load<P: AsRef<Path>>(path: P) -> Result<Self> {
        let path = path.as_ref();

        let (config_path, base_path) = if path.is_dir() {
            (path.join("weavster.yaml"), path.to_path_buf())
        } else {
            (
                path.to_path_buf(),
                path.parent().unwrap_or(Path::new(".")).to_path_buf(),
            )
        };

        if !config_path.exists() {
            return Err(Error::ConfigNotFound {
                path: config_path.display().to_string(),
            });
        }

        let contents = std::fs::read_to_string(&config_path)?;
        let project: ProjectConfig = serde_yaml::from_str(&contents)?;

        Ok(Self { project, base_path })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_default_runtime_mode() {
        let mode = RuntimeMode::default();
        assert_eq!(mode, RuntimeMode::Local);
    }

    #[test]
    fn test_parse_minimal_config() {
        let yaml = r#"
name: test-project
"#;
        let config: ProjectConfig = serde_yaml::from_str(yaml).unwrap();
        assert_eq!(config.name, "test-project");
        assert_eq!(config.version, "0.1.0");
    }

    #[test]
    fn test_parse_full_config() {
        let yaml = r#"
name: test-project
version: "1.0.0"
runtime:
  mode: local
  local:
    data_dir: ".data"
    port: 5434
vars:
  environment: production
"#;
        let config: ProjectConfig = serde_yaml::from_str(yaml).unwrap();
        assert_eq!(config.name, "test-project");
        assert_eq!(config.version, "1.0.0");
        assert_eq!(config.runtime.mode, RuntimeMode::Local);
        assert_eq!(config.runtime.local.port, 5434);
    }
}
