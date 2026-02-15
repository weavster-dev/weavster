//! Integration tests for the complete configuration processing pipeline
//!
//! Tests use temporary directories with real file fixtures to verify:
//! - Project config loading with profiles
//! - Macro expansion in flow files
//! - Static Jinja evaluation
//! - Profile switching and resolution
//! - Connector resolution with profile overrides
//! - Error handling hierarchy

use std::collections::HashMap;
use tempfile::TempDir;
use weavster_core::Config;
use weavster_core::config::{
    ErrorHandlingConfig, JinjaContext, MacroDefinition, OnErrorBehavior, evaluate_static_jinja,
    expand_macros, resolve_error_handling, resolve_profile,
};
use weavster_core::transforms::TransformConfig;

/// Helper to create a temporary project directory with standard structure.
///
/// Returns a `TempDir` that automatically cleans up when dropped.
fn setup_project(_name: &str) -> TempDir {
    let dir = TempDir::new().unwrap();
    std::fs::create_dir_all(dir.path().join("flows")).unwrap();
    std::fs::create_dir_all(dir.path().join("connectors")).unwrap();
    std::fs::create_dir_all(dir.path().join("macros")).unwrap();
    dir
}

// =============================================================================
// Complete Pipeline Tests
// =============================================================================

#[test]
fn test_complete_pipeline_with_dev_profile() {
    let dir = setup_project("pipeline_dev");

    // Write project config with profiles
    std::fs::write(
        dir.path().join("weavster.yaml"),
        r#"
name: integration-test
version: "1.0.0"
vars:
  db_host: localhost
  batch_size: 100
profiles:
  dev:
    vars:
      db_host: dev-db.internal
      debug: true
  prod:
    runtime:
      mode: remote
      remote:
        postgres_url: "postgres://prod-host/db"
    vars:
      db_host: prod-db.internal
      batch_size: 5000
error_handling:
  on_error: log_and_skip
  log_level: warn
"#,
    )
    .unwrap();

    // Write a flow
    std::fs::write(
        dir.path().join("flows/orders.yaml"),
        r#"
name: process_orders
input: file.input
transforms:
  - map:
      order_id: id
      total: amount
  - drop:
      - temp_field
outputs:
  - file.output
"#,
    )
    .unwrap();

    // Write connector config
    std::fs::write(
        dir.path().join("connectors/file.yaml"),
        r#"
input:
  type: file
  path: ./data/orders.jsonl
  format: jsonl
output:
  type: file
  path: ./data/processed.jsonl
  format: jsonl
"#,
    )
    .unwrap();

    // Load with dev profile
    let config = Config::load_with_profile(dir.path(), Some("dev")).unwrap();
    assert_eq!(config.project.name, "integration-test");

    // Verify profile resolution
    let resolved = config.resolved.as_ref().unwrap();
    assert_eq!(resolved.active_profile, Some("dev".to_string()));
    assert_eq!(
        resolved.vars.get("db_host"),
        Some(&serde_yaml::Value::String("dev-db.internal".to_string()))
    );
    // batch_size inherited from global
    assert_eq!(
        resolved.vars.get("batch_size"),
        Some(&serde_yaml::Value::Number(100.into()))
    );

    // Verify flows load correctly
    let flows = config.load_flows().unwrap();
    assert_eq!(flows.len(), 1);
    assert_eq!(flows[0].name, "process_orders");
    assert_eq!(flows[0].transforms.len(), 2);

    // Verify connector resolution
    let input_conn = config.load_connector_config("file.input").unwrap();
    match input_conn {
        weavster_core::connectors::ConnectorConfig::File(f) => {
            assert_eq!(f.path, "./data/orders.jsonl");
        }
        _ => panic!("Expected file connector"),
    }

    // Verify error handling
    assert!(config.project.error_handling.is_some());
    let eh = config.project.error_handling.as_ref().unwrap();
    assert_eq!(eh.on_error, OnErrorBehavior::LogAndSkip);
    assert_eq!(eh.log_level, "warn");
}

#[test]
fn test_profile_switching_dev_to_prod() {
    let dir = setup_project("profile_switch");

    std::fs::write(
        dir.path().join("weavster.yaml"),
        r#"
name: switch-test
vars:
  db_host: localhost
runtime:
  mode: local
profiles:
  dev:
    vars:
      db_host: dev-host
  prod:
    runtime:
      mode: remote
      remote:
        postgres_url: "postgres://prod/db"
    vars:
      db_host: prod-host
"#,
    )
    .unwrap();

    // Load with dev profile
    let dev_config = Config::load_with_profile(dir.path(), Some("dev")).unwrap();
    let dev_resolved = dev_config.resolved.as_ref().unwrap();
    assert_eq!(
        dev_resolved.vars.get("db_host"),
        Some(&serde_yaml::Value::String("dev-host".to_string()))
    );
    assert_eq!(
        dev_resolved.runtime.mode,
        weavster_core::config::RuntimeMode::Local
    );

    // Load with prod profile
    let prod_config = Config::load_with_profile(dir.path(), Some("prod")).unwrap();
    let prod_resolved = prod_config.resolved.as_ref().unwrap();
    assert_eq!(
        prod_resolved.vars.get("db_host"),
        Some(&serde_yaml::Value::String("prod-host".to_string()))
    );
    assert_eq!(
        prod_resolved.runtime.mode,
        weavster_core::config::RuntimeMode::Remote
    );
}

// =============================================================================
// Macro Integration Tests
// =============================================================================

#[test]
fn test_macros_loaded_and_expanded_in_flows() {
    let dir = setup_project("macro_flow");

    std::fs::write(dir.path().join("weavster.yaml"), "name: macro-test\n").unwrap();

    // Write a macro definition
    std::fs::write(
        dir.path().join("macros/normalize.yaml"),
        r#"
name: normalize
description: Normalize field names
transforms:
  - map:
      full_name: name
      email_address: email
"#,
    )
    .unwrap();

    // Load config and verify macros are discoverable
    let config = Config::load(dir.path()).unwrap();
    let macros = config.load_macros().unwrap();
    assert_eq!(macros.len(), 1);
    assert!(macros.contains_key("normalize"));
    assert_eq!(macros["normalize"].transforms.len(), 1);
}

#[test]
fn test_macro_expansion_standalone() {
    let mut macros = HashMap::new();
    macros.insert(
        "cleanup".to_string(),
        MacroDefinition {
            name: "cleanup".to_string(),
            description: None,
            transforms: vec![
                TransformConfig::Drop {
                    drop: vec!["internal_id".to_string(), "debug_info".to_string()],
                    error_handling: None,
                },
                TransformConfig::Map {
                    map: {
                        let mut m = HashMap::new();
                        m.insert("id".to_string(), "external_id".to_string());
                        m
                    },
                    error_handling: None,
                },
            ],
        },
    );

    let yaml = "transforms:\n  {{ macro('cleanup') }}";
    let result = expand_macros(yaml, &macros).unwrap();

    // Verify macro reference is replaced
    assert!(!result.contains("macro('cleanup')"));
    // Verify expanded content contains transform types
    assert!(result.contains("drop"));
    assert!(result.contains("map"));
}

// =============================================================================
// Jinja Integration Tests
// =============================================================================

#[test]
fn test_jinja_evaluation_with_project_vars() {
    let context = JinjaContext {
        vars: {
            let mut m = HashMap::new();
            m.insert(
                "db_host".to_string(),
                serde_yaml::Value::String("mydb.local".to_string()),
            );
            m.insert(
                "db_port".to_string(),
                serde_yaml::Value::Number(5432.into()),
            );
            m
        },
    };

    let yaml = "url: postgres://{{ db_host }}:{{ db_port }}/mydb";
    let result = evaluate_static_jinja(yaml, &context).unwrap();
    assert_eq!(result, "url: postgres://mydb.local:5432/mydb");
}

#[test]
fn test_jinja_preserves_transform_templates() {
    let context = JinjaContext {
        vars: {
            let mut m = HashMap::new();
            m.insert(
                "project_name".to_string(),
                serde_yaml::Value::String("test".to_string()),
            );
            m
        },
    };

    // Simulate a flow YAML with both config-level and runtime-level {{ }} expressions
    let yaml = r#"
name: {{ project_name }}
transforms:
  - template:
      full_name: "{{ first_name }} {{ last_name }}"
      greeting: "Hello {{ title }}"
"#;

    let result = evaluate_static_jinja(yaml, &context).unwrap();

    // project_name should be substituted
    assert!(result.contains("name: test"));
    // Runtime expressions should be preserved
    assert!(result.contains("{{ first_name }}"));
    assert!(result.contains("{{ last_name }}"));
    assert!(result.contains("{{ title }}"));
}

// =============================================================================
// Error Handling Hierarchy Tests
// =============================================================================

#[test]
fn test_error_handling_hierarchy_integration() {
    let global = ErrorHandlingConfig {
        on_error: OnErrorBehavior::LogAndSkip,
        log_level: "error".to_string(),
        retry: None,
    };

    let flow = ErrorHandlingConfig {
        on_error: OnErrorBehavior::StopOnError,
        log_level: "warn".to_string(),
        retry: None,
    };

    // With only global
    let result = resolve_error_handling(Some(&global), None, None);
    assert_eq!(result.on_error, OnErrorBehavior::LogAndSkip);

    // Flow overrides global
    let result = resolve_error_handling(Some(&global), Some(&flow), None);
    assert_eq!(result.on_error, OnErrorBehavior::StopOnError);
    assert_eq!(result.log_level, "warn");
}

// =============================================================================
// Profile Resolution Integration Tests
// =============================================================================

#[test]
fn test_profile_resolution_preserves_unoverridden_values() {
    let yaml = r#"
name: test
version: "2.0.0"
vars:
  a: global_a
  b: global_b
  c: global_c
runtime:
  mode: local
  local:
    port: 5433
profiles:
  minimal:
    vars:
      b: override_b
"#;
    let project: weavster_core::ProjectConfig = serde_yaml::from_str(yaml).unwrap();
    let resolved = resolve_profile(&project, "minimal").unwrap();

    // 'a' and 'c' preserved from global
    assert_eq!(
        resolved.vars.get("a"),
        Some(&serde_yaml::Value::String("global_a".to_string()))
    );
    assert_eq!(
        resolved.vars.get("c"),
        Some(&serde_yaml::Value::String("global_c".to_string()))
    );
    // 'b' overridden by profile
    assert_eq!(
        resolved.vars.get("b"),
        Some(&serde_yaml::Value::String("override_b".to_string()))
    );
    // Runtime inherited from global (no profile override)
    assert_eq!(
        resolved.runtime.mode,
        weavster_core::config::RuntimeMode::Local
    );
    assert_eq!(resolved.runtime.local.port, 5433);
}

// =============================================================================
// Config Caching Integration Tests
// =============================================================================

#[test]
fn test_config_cache_populated_on_load() {
    let dir = setup_project("cache_pop");
    std::fs::write(dir.path().join("weavster.yaml"), "name: cache-test\n").unwrap();

    let config = Config::load(dir.path()).unwrap();
    assert!(config.cache.is_some());

    let cache = config.cache.as_ref().unwrap();
    assert!(!cache.project_hash.is_empty());
    assert_eq!(cache.cached_project.name, "cache-test");
}

#[test]
fn test_reload_detects_project_change() {
    let dir = setup_project("reload_change");
    std::fs::write(dir.path().join("weavster.yaml"), "name: original\n").unwrap();

    let mut config = Config::load(dir.path()).unwrap();
    assert_eq!(config.project.name, "original");

    // Modify the file
    std::fs::write(dir.path().join("weavster.yaml"), "name: updated\n").unwrap();

    let changed = config.reload_if_changed().unwrap();
    assert!(changed);
    assert_eq!(config.project.name, "updated");
}

#[test]
fn test_reload_no_change_is_noop() {
    let dir = setup_project("reload_noop");
    std::fs::write(dir.path().join("weavster.yaml"), "name: stable\n").unwrap();

    let mut config = Config::load(dir.path()).unwrap();
    let changed = config.reload_if_changed().unwrap();
    assert!(!changed);
    assert_eq!(config.project.name, "stable");
}

// =============================================================================
// Bridge Connector Integration Tests
// =============================================================================

#[test]
fn test_bridge_connector_loading() {
    let dir = setup_project("bridge_conn");
    std::fs::write(dir.path().join("weavster.yaml"), "name: bridge-test\n").unwrap();
    std::fs::write(
        dir.path().join("connectors/queue.yaml"),
        r#"
internal:
  type: bridge
  queue_table: flow_bridge
  batch_size: 100
  poll_interval_ms: 500
  lease_duration_ms: 30000
"#,
    )
    .unwrap();

    let config = Config::load(dir.path()).unwrap();
    let conn = config.load_connector_config("queue.internal").unwrap();
    match conn {
        weavster_core::connectors::ConnectorConfig::Bridge(b) => {
            assert_eq!(b.queue_table, "flow_bridge");
            assert_eq!(b.batch_size, Some(100));
            assert_eq!(b.poll_interval_ms, Some(500));
            assert_eq!(b.lease_duration_ms, Some(30000));
        }
        _ => panic!("Expected bridge connector"),
    }
}

// =============================================================================
// Error Path Tests
// =============================================================================

#[test]
fn test_nonexistent_profile_returns_error() {
    let dir = setup_project("bad_profile");
    std::fs::write(
        dir.path().join("weavster.yaml"),
        "name: test\nprofiles:\n  dev:\n    vars:\n      x: y\n",
    )
    .unwrap();

    let result = Config::load_with_profile(dir.path(), Some("staging"));
    assert!(result.is_err());
}

#[test]
fn test_missing_config_file() {
    let dir = setup_project("missing_config");
    // Don't write weavster.yaml
    let result = Config::load(dir.path());
    assert!(result.is_err());
}
