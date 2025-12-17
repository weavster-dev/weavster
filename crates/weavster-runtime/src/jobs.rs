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
