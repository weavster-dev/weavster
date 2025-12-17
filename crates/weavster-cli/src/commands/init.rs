//! Initialize a new Weavster project

use anyhow::Result;
use std::fs;
use std::path::Path;

/// Run the init command
pub async fn run(path: &str, name: Option<&str>) -> Result<()> {
    let project_dir = Path::new(path);

    // Create directory if it doesn't exist
    if !project_dir.exists() {
        fs::create_dir_all(project_dir)?;
    }

    // Get absolute path for deriving name
    let abs_path = project_dir.canonicalize()?;

    // Derive project name from directory name if not provided
    let project_name = match name {
        Some(n) => n.to_string(),
        None => abs_path
            .file_name()
            .and_then(|n| n.to_str())
            .map(|s| s.to_string())
            .ok_or_else(|| anyhow::anyhow!("Could not determine project name from path"))?,
    };

    // Check if already initialized
    if project_dir.join("weavster.yaml").exists() {
        anyhow::bail!(
            "Directory '{}' already contains a weavster.yaml",
            project_dir.display()
        );
    }

    tracing::info!("Creating new Weavster project: {}", project_name);

    // Create directory structure
    fs::create_dir_all(project_dir.join("flows"))?;
    fs::create_dir_all(project_dir.join("connectors"))?;
    fs::create_dir_all(project_dir.join("tests"))?;

    // Create weavster.yaml
    let config = format!(
        r#"# Weavster Project Configuration
name: {project_name}
version: "0.1.0"

runtime:
  mode: local
  local:
    data_dir: ".weavster/data"
    port: 5433

# Global variables available in Jinja templates
vars:
  environment: development
"#
    );
    fs::write(project_dir.join("weavster.yaml"), config)?;

    // Create example flow
    let example_flow = r#"# Example flow
name: example_flow
description: An example flow to get you started

input: file.input

transforms:
  - add_fields:
      processed_at: "{{ now() }}"

  - compute:
      message_length: "len(message)"

outputs:
  - file.output
"#;
    fs::write(project_dir.join("flows/example_flow.yaml"), example_flow)?;

    // Create example connectors
    let connectors = r#"# Connector configurations
# These can be referenced in flows as "file.input", "file.output", etc.

file:
  input:
    type: file
    path: "./data/input.jsonl"
    format: jsonl

  output:
    type: file
    path: "./data/output.jsonl"
    format: jsonl
"#;
    fs::write(project_dir.join("connectors/file.yaml"), connectors)?;

    // Create .gitignore
    let gitignore = r#"# Weavster local data
.weavster/

# Output files
data/output*.jsonl

# IDE
.idea/
.vscode/
*.swp
"#;
    fs::write(project_dir.join(".gitignore"), gitignore)?;

    // Create sample input data
    fs::create_dir_all(project_dir.join("data"))?;
    let sample_data = r#"{"id": 1, "message": "Hello, Weavster!"}
{"id": 2, "message": "This is a test message"}
{"id": 3, "message": "Ready for real-time processing"}
"#;
    fs::write(project_dir.join("data/input.jsonl"), sample_data)?;

    // Create profiles.yaml
    let profiles = r#"# Environment-specific configuration
# Use with: weavster run --profile production

development:
  vars:
    log_level: debug

staging:
  runtime:
    mode: remote
    remote:
      postgres_url: "${STAGING_DATABASE_URL}"
  vars:
    log_level: info

production:
  runtime:
    mode: remote
    remote:
      postgres_url: "${DATABASE_URL}"
      redis_url: "${REDIS_URL}"
  vars:
    log_level: warn
"#;
    fs::write(project_dir.join("profiles.yaml"), profiles)?;

    tracing::info!(
        "✓ Created project '{}' at {}",
        project_name,
        abs_path.display()
    );
    tracing::info!("");
    tracing::info!("Next steps:");
    if path != "." {
        tracing::info!("  cd {}", project_dir.display());
    }
    tracing::info!("  weavster validate    # Check configuration");
    tracing::info!("  weavster run         # Start processing");

    Ok(())
}
