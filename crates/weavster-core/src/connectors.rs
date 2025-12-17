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
    fn test_parse_file_connector_default_format() {
        let yaml = r#"
type: file
path: "./data/input.json"
"#;
        let config: ConnectorConfig = serde_yaml::from_str(yaml).unwrap();
        match config {
            ConnectorConfig::File(f) => {
                assert_eq!(f.path, "./data/input.json");
                assert_eq!(f.format, "jsonl"); // default
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

    #[test]
    fn test_parse_kafka_connector_multiple_brokers() {
        let yaml = r#"
type: kafka
brokers:
  - broker1:9092
  - broker2:9092
  - broker3:9092
topic: events
"#;
        let config: ConnectorConfig = serde_yaml::from_str(yaml).unwrap();
        match config {
            ConnectorConfig::Kafka(k) => {
                assert_eq!(k.brokers.len(), 3);
                assert_eq!(k.brokers[0], "broker1:9092");
                assert_eq!(k.topic, "events");
                assert!(k.group_id.is_none());
            }
            _ => panic!("Expected kafka connector"),
        }
    }

    #[test]
    fn test_parse_http_connector() {
        let yaml = r#"
type: http
url: "https://api.example.com/webhook"
method: POST
headers:
  Authorization: "Bearer token123"
  Content-Type: "application/json"
"#;
        let config: ConnectorConfig = serde_yaml::from_str(yaml).unwrap();
        match config {
            ConnectorConfig::Http(h) => {
                assert_eq!(h.url, "https://api.example.com/webhook");
                assert_eq!(h.method, "POST");
                assert_eq!(h.headers.len(), 2);
                assert_eq!(
                    h.headers.get("Authorization"),
                    Some(&"Bearer token123".to_string())
                );
            }
            _ => panic!("Expected http connector"),
        }
    }

    #[test]
    fn test_parse_http_connector_defaults() {
        let yaml = r#"
type: http
url: "https://api.example.com/events"
"#;
        let config: ConnectorConfig = serde_yaml::from_str(yaml).unwrap();
        match config {
            ConnectorConfig::Http(h) => {
                assert_eq!(h.url, "https://api.example.com/events");
                assert_eq!(h.method, "POST"); // default
                assert!(h.headers.is_empty()); // default empty
            }
            _ => panic!("Expected http connector"),
        }
    }

    #[test]
    fn test_parse_postgres_connector() {
        let yaml = r#"
type: postgres
url: "postgres://user:pass@localhost/db"
table: orders
schema: sales
"#;
        let config: ConnectorConfig = serde_yaml::from_str(yaml).unwrap();
        match config {
            ConnectorConfig::Postgres(p) => {
                assert_eq!(p.url, Some("postgres://user:pass@localhost/db".to_string()));
                assert_eq!(p.table, "orders");
                assert_eq!(p.schema, "sales");
            }
            _ => panic!("Expected postgres connector"),
        }
    }

    #[test]
    fn test_parse_postgres_connector_defaults() {
        let yaml = r#"
type: postgres
table: users
"#;
        let config: ConnectorConfig = serde_yaml::from_str(yaml).unwrap();
        match config {
            ConnectorConfig::Postgres(p) => {
                assert!(p.url.is_none());
                assert_eq!(p.table, "users");
                assert_eq!(p.schema, "public"); // default
            }
            _ => panic!("Expected postgres connector"),
        }
    }

    #[test]
    fn test_message_metadata_default() {
        let metadata = MessageMetadata::default();
        assert!(metadata.source.is_none());
        assert!(metadata.key.is_none());
        assert!(metadata.timestamp.is_none());
        assert!(metadata.extra.is_empty());
    }

    #[test]
    fn test_message_creation() {
        let payload = serde_json::json!({"id": 123, "name": "test"});
        let message = Message {
            payload: payload.clone(),
            metadata: MessageMetadata::default(),
        };
        assert_eq!(message.payload, payload);
    }

    #[test]
    fn test_message_with_metadata() {
        let mut extra = std::collections::HashMap::new();
        extra.insert("partition".to_string(), "0".to_string());
        extra.insert("offset".to_string(), "12345".to_string());

        let metadata = MessageMetadata {
            source: Some("kafka.orders".to_string()),
            key: Some("order-123".to_string()),
            timestamp: Some(1702857600000),
            extra,
        };

        let message = Message {
            payload: serde_json::json!({"order_id": "123"}),
            metadata,
        };

        assert_eq!(message.metadata.source, Some("kafka.orders".to_string()));
        assert_eq!(message.metadata.key, Some("order-123".to_string()));
        assert_eq!(message.metadata.timestamp, Some(1702857600000));
        assert_eq!(
            message.metadata.extra.get("partition"),
            Some(&"0".to_string())
        );
    }

    #[test]
    fn test_message_clone() {
        let message = Message {
            payload: serde_json::json!({"test": "data"}),
            metadata: MessageMetadata {
                source: Some("test".to_string()),
                key: None,
                timestamp: None,
                extra: std::collections::HashMap::new(),
            },
        };

        let cloned = message.clone();
        assert_eq!(message.payload, cloned.payload);
        assert_eq!(message.metadata.source, cloned.metadata.source);
    }

    #[test]
    fn test_file_input_connector_new() {
        let config = FileConnectorConfig {
            path: "/tmp/test.jsonl".to_string(),
            format: "jsonl".to_string(),
        };
        let _connector = FileInputConnector::new(config);
        // Just verify it can be created
    }

    #[test]
    fn test_file_output_connector_new() {
        let config = FileConnectorConfig {
            path: "/tmp/output.jsonl".to_string(),
            format: "jsonl".to_string(),
        };
        let _connector = FileOutputConnector::new(config);
        // Just verify it can be created
    }

    #[tokio::test]
    async fn test_file_input_connector_pull() {
        let config = FileConnectorConfig {
            path: "/tmp/test.jsonl".to_string(),
            format: "jsonl".to_string(),
        };
        let mut connector = FileInputConnector::new(config);

        // Currently returns None (not implemented)
        let result = connector.pull().await.unwrap();
        assert!(result.is_none());
    }

    #[tokio::test]
    async fn test_file_input_connector_ack() {
        let config = FileConnectorConfig {
            path: "/tmp/test.jsonl".to_string(),
            format: "jsonl".to_string(),
        };
        let mut connector = FileInputConnector::new(config);
        let metadata = MessageMetadata::default();

        // Should not error (no-op)
        connector.ack(&metadata).await.unwrap();
    }

    #[tokio::test]
    async fn test_file_input_connector_nack() {
        let config = FileConnectorConfig {
            path: "/tmp/test.jsonl".to_string(),
            format: "jsonl".to_string(),
        };
        let mut connector = FileInputConnector::new(config);
        let metadata = MessageMetadata::default();

        // Should not error (no-op)
        connector.nack(&metadata).await.unwrap();
    }

    #[tokio::test]
    async fn test_file_output_connector_push() {
        let config = FileConnectorConfig {
            path: "/tmp/output.jsonl".to_string(),
            format: "jsonl".to_string(),
        };
        let mut connector = FileOutputConnector::new(config);
        let message = Message {
            payload: serde_json::json!({"test": true}),
            metadata: MessageMetadata::default(),
        };

        // Should not error (not implemented but returns Ok)
        connector.push(message).await.unwrap();
    }

    #[tokio::test]
    async fn test_file_output_connector_flush() {
        let config = FileConnectorConfig {
            path: "/tmp/output.jsonl".to_string(),
            format: "jsonl".to_string(),
        };
        let mut connector = FileOutputConnector::new(config);

        // Should not error (not implemented but returns Ok)
        connector.flush().await.unwrap();
    }

    #[test]
    fn test_connector_config_serialization() {
        let config = ConnectorConfig::File(FileConnectorConfig {
            path: "/data/test.jsonl".to_string(),
            format: "jsonl".to_string(),
        });

        let yaml = serde_yaml::to_string(&config).unwrap();
        assert!(yaml.contains("type: file"));
        assert!(yaml.contains("/data/test.jsonl"));
    }

    #[test]
    fn test_kafka_connector_config_serialization() {
        let config = ConnectorConfig::Kafka(KafkaConnectorConfig {
            brokers: vec!["localhost:9092".to_string()],
            topic: "test-topic".to_string(),
            group_id: Some("test-group".to_string()),
        });

        let yaml = serde_yaml::to_string(&config).unwrap();
        assert!(yaml.contains("type: kafka"));
        assert!(yaml.contains("test-topic"));
        assert!(yaml.contains("test-group"));
    }
}
