//! Flow execution engine

use weavster_core::Config;
use weavster_core::connectors::{
    ConnectorConfig, FileConnectorConfig, FileInputConnector, FileOutputConnector, InputConnector,
    Message, MessageMetadata, OutputConnector,
};
use weavster_core::flow::OutputConfig;

use crate::error::Result;
use crate::state::StateStore;
use crate::wasm::WasmRuntime;
use sha2::{Digest, Sha256};
use std::collections::HashMap;
use std::path::PathBuf;
use std::sync::Arc;

/// Runtime engine for executing flows
pub struct Runtime {
    config: Config,
    wasm_runtime: WasmRuntime,
    db: Arc<dyn StateStore>,
    flow_wasm_paths: HashMap<String, PathBuf>,
}

impl Runtime {
    /// Create a new runtime with the given configuration
    pub fn new(
        config: Config,
        db: Arc<dyn StateStore>,
        flow_wasm_paths: HashMap<String, PathBuf>,
    ) -> Result<Self> {
        let wasm_runtime = WasmRuntime::new()?;
        Ok(Self {
            config,
            wasm_runtime,
            db,
            flow_wasm_paths,
        })
    }

    /// Start the runtime and process all flows
    pub async fn start(&self) -> Result<()> {
        tracing::info!("Starting Weavster runtime");

        let flows = self.config.load_flows()?;
        if flows.is_empty() {
            tracing::warn!("No flows found in flows/ directory");
            return Ok(());
        }

        for flow in &flows {
            tracing::info!("Processing flow: {}", flow.name);

            // Resolve input connector
            let input_config = self.config.load_connector_config(&flow.input)?;

            // File deduplication check!
            let mut resolved_input_path = String::new();
            let mut file_hash = String::new();
            if let ConnectorConfig::File(ref fc) = input_config {
                resolved_input_path = self.resolve_file_path(fc).path;
                let path = resolved_input_path.clone();
                file_hash = tokio::task::spawn_blocking(move || -> anyhow::Result<String> {
                    let mut file = std::fs::File::open(&path)?;
                    let mut hasher = Sha256::new();
                    std::io::copy(&mut file, &mut hasher)?;
                    Ok(hex::encode(hasher.finalize()))
                })
                .await
                .map_err(|e| anyhow::anyhow!("Failed to spawn hashing task: {}", e))??;

                if self
                    .db
                    .is_file_processed(&flow.name, &resolved_input_path, &file_hash)
                    .await?
                {
                    tracing::info!(
                        "Skipping flow '{}', file '{}' already processed",
                        flow.name,
                        resolved_input_path
                    );
                    continue;
                }
            }

            let mut input = self.create_input_connector(input_config)?;

            // Resolve output connectors
            let output_refs: Vec<&str> = flow
                .outputs
                .iter()
                .map(|o| match o {
                    OutputConfig::Simple(s) => s.as_str(),
                    OutputConfig::Conditional { connector, .. } => connector.as_str(),
                })
                .collect();

            let mut outputs: Vec<Box<dyn OutputConnector>> = Vec::new();
            for reference in &output_refs {
                let config = self.config.load_connector_config(reference)?;
                outputs.push(self.create_output_connector(config)?);
            }

            // Get pre-compiled WASM path for this flow
            let wasm_cache_path = match self.flow_wasm_paths.get(&flow.name) {
                Some(path) => path.clone(),
                None => {
                    tracing::error!(
                        "No compiled WASM found for flow '{}'. Make sure it is compiled.",
                        flow.name
                    );
                    continue;
                }
            };

            if !wasm_cache_path.exists() {
                tracing::error!(
                    "WASM file for flow '{}' does not exist at '{}'.",
                    flow.name,
                    wasm_cache_path.display()
                );
                continue;
            }

            // Process messages
            let mut processed: usize = 0;
            let mut failed: usize = 0;

            loop {
                let message = input.pull().await?;
                let Some(msg) = message else { break };

                // 1. Serialize message to bytes payload directly
                let input_bytes = serde_json::to_vec(&msg.payload)?;

                // 2. Call the WASM environment
                match self.wasm_runtime.execute(&wasm_cache_path, &input_bytes) {
                    Ok(result_bytes_vec) => {
                        if result_bytes_vec.is_empty() {
                            continue; // Dropped by filter/WASM logic
                        }

                        // 3. Reserialize into JSON
                        match serde_json::from_slice::<serde_json::Value>(&result_bytes_vec) {
                            Ok(result_json) => {
                                let out_msg = Message {
                                    payload: result_json,
                                    metadata: MessageMetadata::default(),
                                };
                                for output in &mut outputs {
                                    output.push(out_msg.clone()).await?;
                                }
                                processed += 1;
                            }
                            Err(e) => {
                                let policy = weavster_core::config::resolve_error_handling(
                                    self.config.project.error_handling.as_ref(),
                                    flow.error_handling.as_ref(),
                                    None,
                                );
                                if policy.on_error
                                    == weavster_core::config::OnErrorBehavior::StopOnError
                                {
                                    return Err(e.into());
                                }
                                tracing::error!(
                                    "Failed to deserialize return payload on message {}: {}",
                                    processed + 1,
                                    e
                                );
                                failed += 1;
                            }
                        }
                    }
                    Err(e) => {
                        let policy = weavster_core::config::resolve_error_handling(
                            self.config.project.error_handling.as_ref(),
                            flow.error_handling.as_ref(),
                            None,
                        );
                        if policy.on_error == weavster_core::config::OnErrorBehavior::StopOnError {
                            return Err(e);
                        }
                        tracing::error!(
                            "Transform WASM engine error on message {}: {}",
                            processed + 1,
                            e
                        );
                        failed += 1;
                    }
                }
            }

            // Flush all outputs
            for output in &mut outputs {
                output.flush().await?;
            }

            tracing::info!(
                "Flow {} completed: {} processed, {} failed",
                flow.name,
                processed,
                failed
            );

            // Record execution & process file
            if !file_hash.is_empty() {
                self.db
                    .mark_file_processed(&flow.name, &resolved_input_path, &file_hash, processed)
                    .await?;
            }
            self.db
                .record_flow_execution(&flow.name, processed, failed)
                .await?;
        }

        tracing::info!("Runtime finished");
        Ok(())
    }

    /// Gracefully shutdown the runtime
    pub async fn shutdown(&self) -> Result<()> {
        tracing::info!("Shutting down Weavster runtime");
        Ok(())
    }

    fn create_input_connector(&self, config: ConnectorConfig) -> Result<Box<dyn InputConnector>> {
        match config {
            ConnectorConfig::File(fc) => {
                let resolved = self.resolve_file_path(&fc);
                Ok(Box::new(FileInputConnector::new(resolved)))
            }
            _ => anyhow::bail!("Only file connectors are supported in this version"),
        }
    }

    fn create_output_connector(&self, config: ConnectorConfig) -> Result<Box<dyn OutputConnector>> {
        match config {
            ConnectorConfig::File(fc) => {
                let resolved = self.resolve_file_path(&fc);
                Ok(Box::new(FileOutputConnector::new(resolved)))
            }
            _ => anyhow::bail!("Only file connectors are supported in this version"),
        }
    }

    /// Resolve relative file paths against the project base path
    fn resolve_file_path(&self, fc: &FileConnectorConfig) -> FileConnectorConfig {
        let path = if std::path::Path::new(&fc.path).is_relative() {
            self.config
                .base_path
                .join(&fc.path)
                .to_string_lossy()
                .to_string()
        } else {
            fc.path.clone()
        };
        FileConnectorConfig {
            path,
            format: fc.format.clone(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::state::SqliteStateStore;
    use std::collections::HashMap;
    use std::path::PathBuf;
    use weavster_core::config::{Config, LocalConfig, ProjectConfig, RuntimeConfig, RuntimeMode};

    fn test_config() -> Config {
        Config {
            base_path: PathBuf::from("/my/project/dir"),
            project: ProjectConfig {
                name: "test".to_string(),
                version: "0.1.0".to_string(),
                runtime: RuntimeConfig {
                    mode: RuntimeMode::Local,
                    local: LocalConfig {
                        data_dir: ".weavster/data".to_string(),
                        port: 5433,
                    },
                    remote: Default::default(),
                },
                vars: HashMap::new(),
                profiles: HashMap::new(),
                error_handling: None,
                macros_dir: "macros".to_string(),
            },
            resolved: None,
            cache: None,
        }
    }

    // Setup isolated tests using unique paths for Sqlite Migration instances
    #[test]
    fn test_resolve_file_path_relative() {
        let config = test_config();

        let db_dir = tempfile::tempdir().unwrap();
        let db_path = db_dir.path().join("test_rel.db");
        let store =
            Arc::new(SqliteStateStore::new(&format!("sqlite://{}", db_path.display())).unwrap());

        let runtime = Runtime::new(config, store, HashMap::new()).unwrap();

        let fc = FileConnectorConfig {
            path: "data/input.jsonl".to_string(),
            format: "jsonl".to_string(),
        };

        let resolved = runtime.resolve_file_path(&fc);
        assert_eq!(resolved.path, "/my/project/dir/data/input.jsonl");
    }

    #[test]
    fn test_resolve_file_path_absolute() {
        let config = test_config();

        let db_dir = tempfile::tempdir().unwrap();
        let db_path = db_dir.path().join("test_abs.db");
        let store =
            Arc::new(SqliteStateStore::new(&format!("sqlite://{}", db_path.display())).unwrap());

        let runtime = Runtime::new(config, store, HashMap::new()).unwrap();

        let fc = FileConnectorConfig {
            path: "/absolute/path/data.json".to_string(),
            format: "json".to_string(),
        };

        let resolved = runtime.resolve_file_path(&fc);
        assert_eq!(resolved.path, "/absolute/path/data.json");
    }
}
