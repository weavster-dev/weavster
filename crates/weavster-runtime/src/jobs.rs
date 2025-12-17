//! Job definitions for apalis

use serde::{Deserialize, Serialize};

/// Job to execute a flow on a single message
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct FlowJob {
    /// Flow identifier
    pub flow_id: String,

    /// Message payload as JSON string
    pub payload: String,

    /// Message metadata
    pub metadata: JobMetadata,
}

/// Job metadata
#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct JobMetadata {
    /// Source connector
    pub source: Option<String>,

    /// Message key
    pub key: Option<String>,

    /// Original timestamp
    pub timestamp: Option<i64>,

    /// Retry count
    pub retry_count: u32,
}

impl FlowJob {
    /// Create a new flow job
    pub fn new(flow_id: impl Into<String>, payload: impl Into<String>) -> Self {
        Self {
            flow_id: flow_id.into(),
            payload: payload.into(),
            metadata: JobMetadata::default(),
        }
    }

    /// Set the message key
    pub fn with_key(mut self, key: impl Into<String>) -> Self {
        self.metadata.key = Some(key.into());
        self
    }

    /// Set the source connector
    pub fn with_source(mut self, source: impl Into<String>) -> Self {
        self.metadata.source = Some(source.into());
        self
    }
}

// TODO: Implement apalis job handler
//
// async fn execute_flow(job: FlowJob, ctx: JobContext) -> Result<(), Error> {
//     let flow = ctx.data::<FlowRegistry>()?.get(&job.flow_id)?;
//     let message: serde_json::Value = serde_json::from_str(&job.payload)?;
//
//     let result = flow.execute(message).await?;
//
//     for output in flow.outputs() {
//         output.push(result.clone()).await?;
//     }
//
//     Ok(())
// }

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_job_metadata_default() {
        let metadata = JobMetadata::default();
        assert!(metadata.source.is_none());
        assert!(metadata.key.is_none());
        assert!(metadata.timestamp.is_none());
        assert_eq!(metadata.retry_count, 0);
    }

    #[test]
    fn test_flow_job_new() {
        let job = FlowJob::new("test_flow", r#"{"key": "value"}"#);
        assert_eq!(job.flow_id, "test_flow");
        assert_eq!(job.payload, r#"{"key": "value"}"#);
        assert!(job.metadata.source.is_none());
        assert!(job.metadata.key.is_none());
    }

    #[test]
    fn test_flow_job_with_key() {
        let job = FlowJob::new("flow", "{}").with_key("message-key-123");
        assert_eq!(job.metadata.key, Some("message-key-123".to_string()));
    }

    #[test]
    fn test_flow_job_with_source() {
        let job = FlowJob::new("flow", "{}").with_source("kafka.orders");
        assert_eq!(job.metadata.source, Some("kafka.orders".to_string()));
    }

    #[test]
    fn test_flow_job_chained_builders() {
        let job = FlowJob::new("my_flow", r#"{"id": 1}"#)
            .with_key("key-abc")
            .with_source("http.webhooks");

        assert_eq!(job.flow_id, "my_flow");
        assert_eq!(job.payload, r#"{"id": 1}"#);
        assert_eq!(job.metadata.key, Some("key-abc".to_string()));
        assert_eq!(job.metadata.source, Some("http.webhooks".to_string()));
    }

    #[test]
    fn test_flow_job_serialization() {
        let job = FlowJob::new("test", r#"{"data": "test"}"#)
            .with_key("k1")
            .with_source("src");

        let serialized = serde_json::to_string(&job).unwrap();
        let deserialized: FlowJob = serde_json::from_str(&serialized).unwrap();

        assert_eq!(deserialized.flow_id, job.flow_id);
        assert_eq!(deserialized.payload, job.payload);
        assert_eq!(deserialized.metadata.key, job.metadata.key);
        assert_eq!(deserialized.metadata.source, job.metadata.source);
    }

    #[test]
    fn test_job_metadata_serialization() {
        let mut metadata = JobMetadata::default();
        metadata.source = Some("test_source".to_string());
        metadata.key = Some("test_key".to_string());
        metadata.timestamp = Some(1234567890);
        metadata.retry_count = 3;

        let serialized = serde_json::to_string(&metadata).unwrap();
        let deserialized: JobMetadata = serde_json::from_str(&serialized).unwrap();

        assert_eq!(deserialized.source, metadata.source);
        assert_eq!(deserialized.key, metadata.key);
        assert_eq!(deserialized.timestamp, metadata.timestamp);
        assert_eq!(deserialized.retry_count, metadata.retry_count);
    }

    #[test]
    fn test_flow_job_clone() {
        let original = FlowJob::new("flow", "payload").with_key("key");
        let cloned = original.clone();

        assert_eq!(original.flow_id, cloned.flow_id);
        assert_eq!(original.payload, cloned.payload);
        assert_eq!(original.metadata.key, cloned.metadata.key);
    }

    #[test]
    fn test_flow_job_debug() {
        let job = FlowJob::new("test", "{}");
        let debug_str = format!("{:?}", job);
        assert!(debug_str.contains("FlowJob"));
        assert!(debug_str.contains("test"));
    }
}
