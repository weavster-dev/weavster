#![allow(missing_docs)]

use chrono::NaiveDateTime;
use sqlx::FromRow;

#[derive(FromRow, Debug, Clone)]
pub struct ProcessedFile {
    pub id: Option<i32>,
    pub flow_name: String,
    pub file_path: String,
    pub file_hash: String,
    pub processed_at: NaiveDateTime,
    pub record_count: i32,
    pub status: String,
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
    pub status: String,
    pub retry_count: i32,
    pub error_message: Option<String>,
}

#[derive(FromRow, Debug, Clone)]
pub struct FlowExecution {
    pub id: Option<i32>,
    pub flow_name: String,
    pub started_at: NaiveDateTime,
    pub completed_at: Option<NaiveDateTime>,
    pub status: String,
    pub records_processed: i32,
    pub records_failed: i32,
    pub error_message: Option<String>,
}

#[derive(FromRow, Debug, Clone)]
pub struct TestResult {
    pub id: Option<i32>,
    pub test_name: String,
    pub flow_name: String,
    pub executed_at: NaiveDateTime,
    pub status: String,
    pub duration_ms: i32,
    pub error_message: Option<String>,
    pub diff: Option<String>,
}
