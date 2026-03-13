use async_trait::async_trait;
use chrono::Utc;
use diesel::prelude::*;
use diesel_migrations::{EmbeddedMigrations, MigrationHarness, embed_migrations};
use std::sync::{Arc, Mutex};
use tokio::task::spawn_blocking;

use crate::error::Result;
use crate::schema::{flow_executions, processed_files};

pub const SQLITE_MIGRATIONS: EmbeddedMigrations = embed_migrations!("migrations_sqlite");

#[cfg(feature = "postgres")]
pub const POSTGRES_MIGRATIONS: EmbeddedMigrations = embed_migrations!("migrations_postgres");

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
    pool: Arc<Mutex<SqliteConnection>>,
}

impl SqliteStateStore {
    pub fn new(database_url: &str) -> Result<Self> {
        let mut conn = SqliteConnection::establish(database_url)
            .map_err(|e| anyhow::anyhow!("Failed to connect to SQLite: {}", e))?;

        conn.run_pending_migrations(SQLITE_MIGRATIONS)
            .map_err(|e| anyhow::anyhow!("Failed to run SQLite migrations: {}", e))?;

        Ok(Self {
            pool: Arc::new(Mutex::new(conn)),
        })
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
        let flow_name = flow_name.to_string();
        let file_path = file_path.to_string();
        let file_hash = file_hash.to_string();
        let pool = self.pool.clone();

        spawn_blocking(move || -> Result<()> {
            let mut conn = pool
                .lock()
                .map_err(|_| anyhow::anyhow!("DB pool mutex poisoned"))?;
            diesel::insert_into(processed_files::table)
                .values((
                    processed_files::flow_name.eq(&flow_name),
                    processed_files::file_path.eq(&file_path),
                    processed_files::file_hash.eq(&file_hash),
                    processed_files::processed_at.eq(Utc::now().naive_utc()),
                    processed_files::record_count.eq(i32::try_from(record_count)
                        .map_err(|e| anyhow::anyhow!("record_count overflow: {}", e))?),
                    processed_files::status.eq("success"),
                ))
                .execute(&mut *conn)
                .map_err(|e| anyhow::anyhow!("Failed to mark file processed: {}", e))?;
            Ok(())
        })
        .await
        .map_err(|e| anyhow::anyhow!("Failed to spawn blocking task: {}", e))??;

        Ok(())
    }

    async fn is_file_processed(
        &self,
        flow_name: &str,
        file_path: &str,
        file_hash: &str,
    ) -> Result<bool> {
        let flow_name = flow_name.to_string();
        let file_path = file_path.to_string();
        let file_hash = file_hash.to_string();
        let pool = self.pool.clone();

        let is_processed = spawn_blocking(move || -> Result<bool> {
            let mut conn = pool
                .lock()
                .map_err(|_| anyhow::anyhow!("DB pool mutex poisoned"))?;
            let count: i64 = processed_files::table
                .filter(processed_files::flow_name.eq(&flow_name))
                .filter(processed_files::file_path.eq(&file_path))
                .filter(processed_files::file_hash.eq(&file_hash))
                .count()
                .get_result(&mut *conn)
                .map_err(|e| anyhow::anyhow!("Failed to query processed_files: {}", e))?;
            Ok(count > 0)
        })
        .await
        .map_err(|e| anyhow::anyhow!("Failed to spawn blocking task: {}", e))??;

        Ok(is_processed)
    }

    async fn record_flow_execution(
        &self,
        flow_name: &str,
        records_processed: usize,
        records_failed: usize,
    ) -> Result<()> {
        let flow_name = flow_name.to_string();
        let pool = self.pool.clone();

        spawn_blocking(move || -> Result<()> {
            let mut conn = pool
                .lock()
                .map_err(|_| anyhow::anyhow!("DB pool mutex poisoned"))?;
            diesel::insert_into(flow_executions::table)
                .values((
                    flow_executions::flow_name.eq(&flow_name),
                    flow_executions::started_at.eq(Utc::now().naive_utc()),
                    flow_executions::status.eq("completed"),
                    flow_executions::records_processed.eq(i32::try_from(records_processed)
                        .map_err(|e| anyhow::anyhow!("records_processed overflow: {}", e))?),
                    flow_executions::records_failed.eq(i32::try_from(records_failed)
                        .map_err(|e| anyhow::anyhow!("records_failed overflow: {}", e))?),
                ))
                .execute(&mut *conn)
                .map_err(|e| anyhow::anyhow!("Failed to record flow execution: {}", e))?;
            Ok(())
        })
        .await
        .map_err(|e| anyhow::anyhow!("Failed to spawn blocking task: {}", e))??;

        Ok(())
    }
}

#[cfg(feature = "postgres")]
/// Postgres state store for production
pub struct PostgresStateStore {
    pool: Arc<Mutex<PgConnection>>,
}

#[cfg(feature = "postgres")]
impl PostgresStateStore {
    pub fn new(database_url: &str) -> Result<Self> {
        let mut conn = PgConnection::establish(database_url)
            .map_err(|e| anyhow::anyhow!("Failed to connect to Postgres: {}", e))?;

        conn.run_pending_migrations(POSTGRES_MIGRATIONS)
            .map_err(|e| anyhow::anyhow!("Failed to run Postgres migrations: {}", e))?;

        Ok(Self {
            pool: Arc::new(Mutex::new(conn)),
        })
    }
}

#[cfg(feature = "postgres")]
#[async_trait]
impl StateStore for PostgresStateStore {
    async fn mark_file_processed(
        &self,
        flow_name: &str,
        file_path: &str,
        file_hash: &str,
        record_count: usize,
    ) -> Result<()> {
        let flow_name = flow_name.to_string();
        let file_path = file_path.to_string();
        let file_hash = file_hash.to_string();
        let pool = self.pool.clone();

        spawn_blocking(move || -> Result<()> {
            let mut conn = pool
                .lock()
                .map_err(|_| anyhow::anyhow!("DB pool mutex poisoned"))?;
            diesel::insert_into(processed_files::table)
                .values((
                    processed_files::flow_name.eq(&flow_name),
                    processed_files::file_path.eq(&file_path),
                    processed_files::file_hash.eq(&file_hash),
                    processed_files::processed_at.eq(Utc::now().naive_utc()),
                    processed_files::record_count.eq(i32::try_from(record_count)
                        .map_err(|e| anyhow::anyhow!("record_count overflow: {}", e))?),
                    processed_files::status.eq("success"),
                ))
                .execute(&mut *conn)
                .map_err(|e| anyhow::anyhow!("Failed to mark file processed: {}", e))?;
            Ok(())
        })
        .await
        .map_err(|e| anyhow::anyhow!("Failed to spawn blocking task: {}", e))??;

        Ok(())
    }

    async fn is_file_processed(
        &self,
        flow_name: &str,
        file_path: &str,
        file_hash: &str,
    ) -> Result<bool> {
        let flow_name = flow_name.to_string();
        let file_path = file_path.to_string();
        let file_hash = file_hash.to_string();
        let pool = self.pool.clone();

        let is_processed = spawn_blocking(move || -> Result<bool> {
            let mut conn = pool
                .lock()
                .map_err(|_| anyhow::anyhow!("DB pool mutex poisoned"))?;
            let count: i64 = processed_files::table
                .filter(processed_files::flow_name.eq(&flow_name))
                .filter(processed_files::file_path.eq(&file_path))
                .filter(processed_files::file_hash.eq(&file_hash))
                .count()
                .get_result(&mut *conn)
                .map_err(|e| anyhow::anyhow!("Failed to query processed_files: {}", e))?;
            Ok(count > 0)
        })
        .await
        .map_err(|e| anyhow::anyhow!("Failed to spawn blocking task: {}", e))??;

        Ok(is_processed)
    }

    async fn record_flow_execution(
        &self,
        flow_name: &str,
        records_processed: usize,
        records_failed: usize,
    ) -> Result<()> {
        let flow_name = flow_name.to_string();
        let pool = self.pool.clone();

        spawn_blocking(move || -> Result<()> {
            let mut conn = pool
                .lock()
                .map_err(|_| anyhow::anyhow!("DB pool mutex poisoned"))?;
            diesel::insert_into(flow_executions::table)
                .values((
                    flow_executions::flow_name.eq(&flow_name),
                    flow_executions::started_at.eq(Utc::now().naive_utc()),
                    flow_executions::status.eq("completed"),
                    flow_executions::records_processed.eq(i32::try_from(records_processed)
                        .map_err(|e| anyhow::anyhow!("records_processed overflow: {}", e))?),
                    flow_executions::records_failed.eq(i32::try_from(records_failed)
                        .map_err(|e| anyhow::anyhow!("records_failed overflow: {}", e))?),
                ))
                .execute(&mut *conn)
                .map_err(|e| anyhow::anyhow!("Failed to record flow execution: {}", e))?;
            Ok(())
        })
        .await
        .map_err(|e| anyhow::anyhow!("Failed to spawn blocking task: {}", e))??;

        Ok(())
    }
}
