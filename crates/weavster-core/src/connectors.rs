//! Connector trait and implementations
//!
//! Connectors are adapters for external systems (Kafka, Postgres, HTTP, etc.)

use async_trait::async_trait;
use serde::{Deserialize, Serialize};

use crate::error::Result;

/// Message from or to a connector
#[derive(Debug, Clone)]
pub struct Message {
    /// Message payload as JSON
    pub payload: serde_json::Value,

    /// Message metadata (e.g., Kafka offset, timestamp)
    pub metadata: MessageMetadata,
}

/// Message metadata
#[derive(Debug, Clone, Default)]
pub struct MessageMetadata {
    /// Source connector name
    pub source: Option<String>,

    /// Message key (for keyed systems like Kafka)
    pub key: Option<String>,

    /// Message timestamp
    pub timestamp: Option<i64>,

    /// Additional metadata as key-value pairs
    pub extra: std::collections::HashMap<String, String>,
}

/// Trait for input connectors (sources)
#[async_trait]
pub trait InputConnector: Send + Sync {
    /// Pull the next message from the source
    async fn pull(&mut self) -> Result<Option<Message>>;

    /// Acknowledge a message was processed successfully
    async fn ack(&mut self, metadata: &MessageMetadata) -> Result<()>;

    /// Negative acknowledgment - message processing failed
    async fn nack(&mut self, metadata: &MessageMetadata) -> Result<()>;
}

/// Trait for output connectors (sinks)
#[async_trait]
pub trait OutputConnector: Send + Sync {
    /// Push a message to the sink
    async fn push(&mut self, message: Message) -> Result<()>;

    /// Flush any buffered messages
    async fn flush(&mut self) -> Result<()>;
}

/// Connector configuration from YAML
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum ConnectorConfig {
    /// File connector for local development/testing
    File(FileConnectorConfig),

    /// HTTP webhook connector
    Http(HttpConnectorConfig),

    /// Kafka connector
    Kafka(KafkaConnectorConfig),

    /// PostgreSQL connector
    Postgres(PostgresConnectorConfig),
}

/// File connector configuration
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct FileConnectorConfig {
    /// File path
    pub path: String,

    /// Format: json, jsonl, csv
    #[serde(default = "default_file_format")]
    pub format: String,
}

fn default_file_format() -> String {
    "jsonl".to_string()
}

/// HTTP connector configuration
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct HttpConnectorConfig {
    /// URL endpoint
    pub url: String,

    /// HTTP method (GET, POST, etc.)
    #[serde(default = "default_http_method")]
    pub method: String,

    /// Headers
    #[serde(default)]
    pub headers: std::collections::HashMap<String, String>,
}

fn default_http_method() -> String {
    "POST".to_string()
}

/// Kafka connector configuration
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct KafkaConnectorConfig {
    /// Bootstrap servers
    pub brokers: Vec<String>,

    /// Topic name
    pub topic: String,

    /// Consumer group (for input)
    pub group_id: Option<String>,
}

/// PostgreSQL connector configuration
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PostgresConnectorConfig {
    /// Connection URL or reference to profile
    pub url: Option<String>,

    /// Table name
    pub table: String,

    /// Schema name
    #[serde(default = "default_pg_schema")]
    pub schema: String,
}

fn default_pg_schema() -> String {
    "public".to_string()
}

// ============================================================================
// File Connector Implementation (for local dev/testing)
// ============================================================================

/// File-based input connector
pub struct FileInputConnector {
    #[allow(dead_code)]
    config: FileConnectorConfig,
    // TODO: Add file reader state
}

impl FileInputConnector {
    /// Create a new file input connector
    pub fn new(config: FileConnectorConfig) -> Self {
        Self { config }
    }
}

#[async_trait]
impl InputConnector for FileInputConnector {
    async fn pull(&mut self) -> Result<Option<Message>> {
        // TODO: Implement file reading
        Ok(None)
    }

    async fn ack(&mut self, _metadata: &MessageMetadata) -> Result<()> {
        // No-op for file connector
        Ok(())
    }

    async fn nack(&mut self, _metadata: &MessageMetadata) -> Result<()> {
        // No-op for file connector
        Ok(())
    }
}

/// File-based output connector
pub struct FileOutputConnector {
    #[allow(dead_code)]
    config: FileConnectorConfig,
    // TODO: Add file writer state
}

impl FileOutputConnector {
    /// Create a new file output connector
    pub fn new(config: FileConnectorConfig) -> Self {
        Self { config }
    }
}

#[async_trait]
impl OutputConnector for FileOutputConnector {
    async fn push(&mut self, _message: Message) -> Result<()> {
        // TODO: Implement file writing
        Ok(())
    }

    async fn flush(&mut self) -> Result<()> {
        // TODO: Flush file buffer
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_file_connector() {
        let yaml = r#"
type: file
path: "./data/input.jsonl"
format: jsonl
"#;
        let config: ConnectorConfig = serde_yaml::from_str(yaml).unwrap();
        match config {
            ConnectorConfig::File(f) => {
                assert_eq!(f.path, "./data/input.jsonl");
                assert_eq!(f.format, "jsonl");
            }
            _ => panic!("Expected file connector"),
        }
    }

    #[test]
    fn test_parse_kafka_connector() {
        let yaml = r#"
type: kafka
brokers:
  - localhost:9092
topic: orders
group_id: weavster-consumer
"#;
        let config: ConnectorConfig = serde_yaml::from_str(yaml).unwrap();
        match config {
            ConnectorConfig::Kafka(k) => {
                assert_eq!(k.topic, "orders");
                assert_eq!(k.group_id, Some("weavster-consumer".to_string()));
            }
            _ => panic!("Expected kafka connector"),
        }
    }
}
