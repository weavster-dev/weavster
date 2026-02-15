//! Flow execution engine

use weavster_core::Config;
use weavster_core::connectors::{
    ConnectorConfig, FileConnectorConfig, FileInputConnector, FileOutputConnector, InputConnector,
    Message, MessageMetadata, OutputConnector,
};
use weavster_core::flow::OutputConfig;

use crate::error::Result;

/// Runtime engine for executing flows
pub struct Runtime {
    config: Config,
}

impl Runtime {
    /// Create a new runtime with the given configuration
    pub fn new(config: Config) -> Self {
        Self { config }
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

            // Process messages
            let mut processed: usize = 0;
            let mut failed: usize = 0;

            loop {
                let message = input.pull().await?;
                let Some(msg) = message else { break };

                match weavster_core::interpreter::apply_transforms(
                    &msg.payload,
                    &flow.transforms,
                    flow.dynamic_context.as_ref(),
                ) {
                    Ok(result) => {
                        let out_msg = Message {
                            payload: result,
                            metadata: MessageMetadata::default(),
                        };
                        for output in &mut outputs {
                            output.push(out_msg.clone()).await?;
                        }
                        processed += 1;
                    }
                    Err(e) => {
                        tracing::error!("Transform error on message {}: {}", processed + 1, e);
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
