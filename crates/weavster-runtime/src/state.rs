#![allow(missing_docs)]

use async_trait::async_trait;
use chrono::Utc;
use sqlx::{PgPool, SqlitePool};

use crate::error::Result;

#[async_trait]
pub trait StateStore: Send + Sync {
    async fn mark_file_processed(
        &self,
        flow_name: &str,
        file_path: &str,
        file_hash: &str,
        record_count: usize,
    ) -> Result<()>;

    async fn is_file_processed(
        &self,
        flow_name: &str,
        file_path: &str,
        file_hash: &str,
    ) -> Result<bool>;

    async fn record_flow_execution(
        &self,
        flow_name: &str,
        records_processed: usize,
        records_failed: usize,
    ) -> Result<()>;
}

/// SQLite state store for local dev
pub struct SqliteStateStore {
    pool: SqlitePool,
}

impl SqliteStateStore {
    pub async fn new(database_url: &str) -> Result<Self> {
        let pool = SqlitePool::connect(database_url)
            .await
            .map_err(|e| anyhow::anyhow!("Failed to connect to SQLite: {}", e))?;

        sqlx::migrate!("./migrations_sqlite")
            .run(&pool)
            .await
            .map_err(|e| anyhow::anyhow!("Failed to run SQLite migrations: {}", e))?;

        Ok(Self { pool })
    }
}

#[async_trait]
impl StateStore for SqliteStateStore {
    async fn mark_file_processed(
        &self,
        flow_name: &str,
        file_path: &str,
        file_hash: &str,
        record_count: usize,
    ) -> Result<()> {
        let record_count = i32::try_from(record_count)
            .map_err(|e| anyhow::anyhow!("record_count overflow: {}", e))?;

        sqlx::query(
            "INSERT INTO processed_files (flow_name, file_path, file_hash, processed_at, record_count, status)
             VALUES ($1, $2, $3, $4, $5, $6)"
        )
        .bind(flow_name)
        .bind(file_path)
        .bind(file_hash)
        .bind(Utc::now().naive_utc())
        .bind(record_count)
        .bind("success")
        .execute(&self.pool)
        .await
        .map_err(|e| anyhow::anyhow!("Failed to mark file processed: {}", e))?;

        Ok(())
    }

    async fn is_file_processed(
        &self,
        flow_name: &str,
        file_path: &str,
        file_hash: &str,
    ) -> Result<bool> {
        let result: (i64,) = sqlx::query_as(
            "SELECT COUNT(*) FROM processed_files
             WHERE flow_name = $1 AND file_path = $2 AND file_hash = $3",
        )
        .bind(flow_name)
        .bind(file_path)
        .bind(file_hash)
        .fetch_one(&self.pool)
        .await
        .map_err(|e| anyhow::anyhow!("Failed to query processed_files: {}", e))?;

        Ok(result.0 > 0)
    }

    async fn record_flow_execution(
        &self,
        flow_name: &str,
        records_processed: usize,
        records_failed: usize,
    ) -> Result<()> {
        let records_processed = i32::try_from(records_processed)
            .map_err(|e| anyhow::anyhow!("records_processed overflow: {}", e))?;
        let records_failed = i32::try_from(records_failed)
            .map_err(|e| anyhow::anyhow!("records_failed overflow: {}", e))?;

        sqlx::query(
            "INSERT INTO flow_executions (flow_name, started_at, status, records_processed, records_failed)
             VALUES ($1, $2, $3, $4, $5)"
        )
        .bind(flow_name)
        .bind(Utc::now().naive_utc())
        .bind("completed")
        .bind(records_processed)
        .bind(records_failed)
        .execute(&self.pool)
        .await
        .map_err(|e| anyhow::anyhow!("Failed to record flow execution: {}", e))?;

        Ok(())
    }
}

/// Postgres state store for production
pub struct PostgresStateStore {
    pool: PgPool,
}

impl PostgresStateStore {
    pub async fn new(database_url: &str) -> Result<Self> {
        let pool = PgPool::connect(database_url)
            .await
            .map_err(|e| anyhow::anyhow!("Failed to connect to Postgres: {}", e))?;

        sqlx::migrate!("./migrations_postgres")
            .run(&pool)
            .await
            .map_err(|e| anyhow::anyhow!("Failed to run Postgres migrations: {}", e))?;

        Ok(Self { pool })
    }
}

#[async_trait]
impl StateStore for PostgresStateStore {
    async fn mark_file_processed(
        &self,
        flow_name: &str,
        file_path: &str,
        file_hash: &str,
        record_count: usize,
    ) -> Result<()> {
        let record_count = i32::try_from(record_count)
            .map_err(|e| anyhow::anyhow!("record_count overflow: {}", e))?;

        sqlx::query(
            "INSERT INTO processed_files (flow_name, file_path, file_hash, processed_at, record_count, status)
             VALUES ($1, $2, $3, $4, $5, $6)"
        )
        .bind(flow_name)
        .bind(file_path)
        .bind(file_hash)
        .bind(Utc::now().naive_utc())
        .bind(record_count)
        .bind("success")
        .execute(&self.pool)
        .await
        .map_err(|e| anyhow::anyhow!("Failed to mark file processed: {}", e))?;

        Ok(())
    }

    async fn is_file_processed(
        &self,
        flow_name: &str,
        file_path: &str,
        file_hash: &str,
    ) -> Result<bool> {
        let result: (i64,) = sqlx::query_as(
            "SELECT COUNT(*) FROM processed_files
             WHERE flow_name = $1 AND file_path = $2 AND file_hash = $3",
        )
        .bind(flow_name)
        .bind(file_path)
        .bind(file_hash)
        .fetch_one(&self.pool)
        .await
        .map_err(|e| anyhow::anyhow!("Failed to query processed_files: {}", e))?;

        Ok(result.0 > 0)
    }

    async fn record_flow_execution(
        &self,
        flow_name: &str,
        records_processed: usize,
        records_failed: usize,
    ) -> Result<()> {
        let records_processed = i32::try_from(records_processed)
            .map_err(|e| anyhow::anyhow!("records_processed overflow: {}", e))?;
        let records_failed = i32::try_from(records_failed)
            .map_err(|e| anyhow::anyhow!("records_failed overflow: {}", e))?;

        sqlx::query(
            "INSERT INTO flow_executions (flow_name, started_at, status, records_processed, records_failed)
             VALUES ($1, $2, $3, $4, $5)"
        )
        .bind(flow_name)
        .bind(Utc::now().naive_utc())
        .bind("completed")
        .bind(records_processed)
        .bind(records_failed)
        .execute(&self.pool)
        .await
        .map_err(|e| anyhow::anyhow!("Failed to record flow execution: {}", e))?;

        Ok(())
    }
}
