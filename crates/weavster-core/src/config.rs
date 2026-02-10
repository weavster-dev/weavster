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

use crate::connectors::ConnectorConfig;
use crate::error::{Error, Result};
use crate::flow::Flow;

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

    /// Load all flow definitions from `flows/*.yaml`
    pub fn load_flows(&self) -> Result<Vec<Flow>> {
        let flows_dir = self.base_path.join("flows");
        if !flows_dir.exists() {
            return Ok(vec![]);
        }

        let mut flows = Vec::new();
        let mut entries: Vec<_> = std::fs::read_dir(&flows_dir)?
            .filter_map(|e| e.ok())
            .filter(|e| {
                e.path()
                    .extension()
                    .is_some_and(|ext| ext == "yaml" || ext == "yml")
            })
            .collect();
        entries.sort_by_key(|e| e.path());

        for entry in entries {
            let contents = std::fs::read_to_string(entry.path())?;
            let flow: Flow = serde_yaml::from_str(&contents)?;
            flows.push(flow);
        }
        Ok(flows)
    }

    /// Resolve a dotted connector reference like `"file.input"` to a `ConnectorConfig`.
    ///
    /// The reference format is `"<filename>.<key>"` which maps to
    /// `connectors/<filename>.yaml` → key `<key>`.
    pub fn load_connector_config(&self, reference: &str) -> Result<ConnectorConfig> {
        let (file, key) = reference
            .split_once('.')
            .ok_or_else(|| Error::ConfigInvalid {
                message: format!(
                    "connector reference '{}' must be in 'file.key' format",
                    reference
                ),
            })?;

        let path = self
            .base_path
            .join("connectors")
            .join(format!("{}.yaml", file));
        if !path.exists() {
            return Err(Error::ConfigNotFound {
                path: path.display().to_string(),
            });
        }

        let contents = std::fs::read_to_string(&path)?;
        let doc: serde_yaml::Value = serde_yaml::from_str(&contents)?;

        let connector_value = doc.get(key).ok_or_else(|| Error::ConfigInvalid {
            message: format!("key '{}' not found in {}", key, path.display()),
        })?;

        let config: ConnectorConfig = serde_yaml::from_value(connector_value.clone())?;
        Ok(config)
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

    #[test]
    fn test_load_flows_from_dir() {
        let dir = std::env::temp_dir().join("weavster_test_flows");
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(dir.join("flows")).unwrap();
        std::fs::write(dir.join("weavster.yaml"), "name: test\n").unwrap();
        std::fs::write(
            dir.join("flows/a.yaml"),
            "name: flow_a\ninput: file.input\noutputs:\n  - file.output\n",
        )
        .unwrap();

        let config = Config::load(&dir).unwrap();
        let flows = config.load_flows().unwrap();
        assert_eq!(flows.len(), 1);
        assert_eq!(flows[0].name, "flow_a");

        std::fs::remove_dir_all(&dir).ok();
    }

    #[test]
    fn test_load_connector_config() {
        let dir = std::env::temp_dir().join("weavster_test_conn");
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(dir.join("connectors")).unwrap();
        std::fs::write(dir.join("weavster.yaml"), "name: test\n").unwrap();
        std::fs::write(
            dir.join("connectors/file.yaml"),
            "input:\n  type: file\n  path: ./data/in.jsonl\n  format: jsonl\noutput:\n  type: file\n  path: ./data/out.jsonl\n  format: jsonl\n",
        )
        .unwrap();

        let config = Config::load(&dir).unwrap();
        let conn = config.load_connector_config("file.input").unwrap();
        match conn {
            crate::connectors::ConnectorConfig::File(f) => {
                assert_eq!(f.path, "./data/in.jsonl");
            }
            _ => panic!("Expected file connector"),
        }

        std::fs::remove_dir_all(&dir).ok();
    }

    #[test]
    fn test_load_connector_config_bad_reference() {
        let dir = std::env::temp_dir().join("weavster_test_conn_bad");
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        std::fs::write(dir.join("weavster.yaml"), "name: test\n").unwrap();

        let config = Config::load(&dir).unwrap();
        let result = config.load_connector_config("noformat");
        assert!(result.is_err());

        std::fs::remove_dir_all(&dir).ok();
    }
}
