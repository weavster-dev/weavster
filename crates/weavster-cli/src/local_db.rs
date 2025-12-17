//! Local embedded PostgreSQL database

use anyhow::{Context, Result};
use postgresql_embedded::{PostgreSQL, Settings};
use sqlx::PgPool;
use std::path::PathBuf;
use weavster_core::config::LocalConfig;

/// Wrapper around embedded PostgreSQL
pub struct LocalDatabase {
    #[allow(dead_code)]
    pg: PostgreSQL,
    pool: PgPool,
}

impl LocalDatabase {
    /// Start a local embedded PostgreSQL instance
    pub async fn new(config: &LocalConfig) -> Result<Self> {
        let data_dir = PathBuf::from(&config.data_dir);

        // Ensure data directory exists
        std::fs::create_dir_all(&data_dir).context("Failed to create data directory")?;

        tracing::debug!("Initializing PostgreSQL in {:?}", data_dir);

        // Configure embedded PostgreSQL
        let settings = Settings {
            installation_dir: data_dir.join("pg"),
            data_dir: data_dir.join("data"),
            port: config.port,
            username: "weavster".to_string(),
            password: "weavster".to_string(),
            ..Default::default()
        };

        // Start PostgreSQL
        let mut pg = PostgreSQL::new(settings);

        tracing::info!("Setting up PostgreSQL (this may take a moment on first run)...");
        pg.setup().await.context("Failed to setup PostgreSQL")?;

        tracing::debug!("Starting PostgreSQL server...");
        pg.start().await.context("Failed to start PostgreSQL")?;

        // Create weavster database if it doesn't exist
        let db_name = "weavster";
        if !pg
            .database_exists(db_name)
            .await
            .context("Failed to check database existence")?
        {
            tracing::debug!("Creating database '{}'", db_name);
            pg.create_database(db_name)
                .await
                .context("Failed to create database")?;
        }

        // Build connection URL
        let url = format!(
            "postgres://weavster:weavster@localhost:{}/{}",
            config.port, db_name
        );

        tracing::debug!("Connecting to database...");
        let pool = PgPool::connect(&url)
            .await
            .context("Failed to connect to local database")?;

        tracing::info!("Local database ready on port {}", config.port);

        Ok(Self { pg, pool })
    }

    /// Get a reference to the connection pool
    pub fn pool(&self) -> &PgPool {
        &self.pool
    }

    /// Get the database URL
    #[allow(dead_code)]
    pub fn url(&self) -> String {
        // TODO: Get from settings
        "postgres://weavster:weavster@localhost:5433/weavster".to_string()
    }
}

impl Drop for LocalDatabase {
    fn drop(&mut self) {
        // PostgreSQL will be stopped when the pg field is dropped
        tracing::debug!("Stopping local database");
    }
}
