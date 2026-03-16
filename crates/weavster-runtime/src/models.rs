#![allow(missing_docs)]

use chrono::NaiveDateTime;
use serde::{Deserialize, Serialize};
use sqlx::FromRow;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, sqlx::Type)]
#[sqlx(rename_all = "lowercase")]
pub enum ProcessingStatus {
    Ready,
    Processing,
    Completed,
    Failed,
    Skipped,
}

#[derive(FromRow, Debug, Clone)]
pub struct ProcessedFile {
    pub id: Option<i32>,
    pub flow_name: String,
    pub file_path: String,
    pub file_hash: String,
    pub processed_at: NaiveDateTime,
    pub record_count: i64,
    pub status: ProcessingStatus,
    pub error_message: Option<String>,
}

#[derive(FromRow, Debug, Clone)]
pub struct BridgeMessage {
    pub id: Option<i32>,
    pub bridge_name: String,
    pub message_id: String,
    pub payload: Vec<u8>,
    pub created_at: NaiveDateTime,
    pub processed_at: Option<NaiveDateTime>,
    pub status: ProcessingStatus,
    pub retry_count: i32,
    pub error_message: Option<String>,
}

#[derive(FromRow, Debug, Clone)]
pub struct FlowExecution {
    pub id: Option<i32>,
    pub flow_name: String,
    pub started_at: NaiveDateTime,
    pub completed_at: Option<NaiveDateTime>,
    pub status: ProcessingStatus,
    pub records_processed: i64,
    pub records_failed: i64,
    pub error_message: Option<String>,
}

#[derive(FromRow, Debug, Clone)]
pub struct TestResult {
    pub id: Option<i32>,
    pub test_name: String,
    pub flow_name: String,
    pub executed_at: NaiveDateTime,
    pub status: ProcessingStatus,
    pub duration_ms: i32,
    pub error_message: Option<String>,
    pub diff: Option<String>,
}
