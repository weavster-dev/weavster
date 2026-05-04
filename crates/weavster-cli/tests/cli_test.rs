use assert_cmd::cargo::cargo_bin_cmd;
use predicates::prelude::*;
use std::fs;

#[test]
fn test_init_and_run() {
    let dir = tempfile::tempdir().unwrap();

    // Init project
    cargo_bin_cmd!("weavster")
        .args(["init", dir.path().to_str().unwrap()])
        .assert()
        .success();

    // Verify generated files exist
    assert!(dir.path().join("weavster.yaml").exists());
    assert!(dir.path().join("flows/example_flow.yaml").exists());
    assert!(dir.path().join("connectors/file.yaml").exists());
    assert!(dir.path().join("data/input.jsonl").exists());

    let config = std::fs::read_to_string(dir.path().join("weavster.yaml")).unwrap();
    let config_yaml: serde_yaml::Value = serde_yaml::from_str(&config).unwrap();
    let local_config = config_yaml
        .get("runtime")
        .and_then(|runtime| runtime.get("local"))
        .and_then(serde_yaml::Value::as_mapping)
        .expect("generated weavster.yaml should contain runtime.local");
    assert!(
        !local_config.contains_key(serde_yaml::Value::String("port".to_string())),
        "generated weavster.yaml should omit runtime.local.port"
    );

    // Run the generated project
    cargo_bin_cmd!("weavster")
        .args(["--config", dir.path().to_str().unwrap(), "run"])
        .assert()
        .success();

    // Verify output file was created
    let output_path = dir.path().join("data/output.jsonl");
    assert!(output_path.exists(), "output.jsonl should exist");

    // Verify output contents
    let output = std::fs::read_to_string(&output_path).unwrap();
    let lines: Vec<serde_json::Value> = output
        .lines()
        .filter(|l| !l.is_empty())
        .map(|l| serde_json::from_str(l).unwrap())
        .collect();

    assert_eq!(lines.len(), 3);

    // First record: Alice
    assert_eq!(lines[0]["full_name"], "Alice Johnson");
    assert_eq!(lines[0]["email"], "alice@example.com");
    assert_eq!(lines[0]["processed"], true);
    assert!(lines[0].get("name").is_none(), "name should be dropped");
    assert!(lines[0].get("age").is_none(), "age should be dropped");

    // Second record: Bob
    assert_eq!(lines[1]["full_name"], "Bob Smith");
    assert_eq!(lines[1]["email"], "bob@example.com");
    assert_eq!(lines[1]["processed"], true);
    assert!(lines[1].get("name").is_none(), "name should be dropped");
    assert!(lines[1].get("age").is_none(), "age should be dropped");

    // Third record: Carol
    assert_eq!(lines[2]["full_name"], "Carol Williams");
    assert_eq!(lines[2]["email"], "carol@example.com");
    assert_eq!(lines[2]["processed"], true);
    assert!(lines[2].get("name").is_none(), "name should be dropped");
    assert!(lines[2].get("age").is_none(), "age should be dropped");
}

#[test]
fn test_run_rejects_removed_flags() {
    let dir = tempfile::tempdir().unwrap();

    cargo_bin_cmd!("weavster")
        .args(["init", dir.path().to_str().unwrap()])
        .assert()
        .success();

    let unexpected_argument = predicate::str::contains("unexpected argument");

    cargo_bin_cmd!("weavster")
        .args(["--config", dir.path().to_str().unwrap(), "run", "--once"])
        .assert()
        .failure()
        .stderr(
            unexpected_argument
                .clone()
                .and(predicate::str::contains("--once")),
        );

    cargo_bin_cmd!("weavster")
        .args([
            "--config",
            dir.path().to_str().unwrap(),
            "run",
            "--flow",
            "example_flow",
        ])
        .assert()
        .failure()
        .stderr(unexpected_argument.and(predicate::str::contains("--flow")));
}

#[test]
fn test_test_command_uses_config_project_for_relative_fixtures() {
    let dir = tempfile::tempdir().unwrap();

    cargo_bin_cmd!("weavster")
        .args(["init", dir.path().to_str().unwrap()])
        .assert()
        .success();

    let expected_output_path = dir.path().join("tests/expected_output.jsonl");
    fs::write(
        &expected_output_path,
        r#"{"full_name":"Alice Johnson","email":"alice@example.com","processed":true}
{"full_name":"Bob Smith","email":"bob@example.com","processed":true}
{"full_name":"Carol Williams","email":"carol@example.com","processed":true}
"#,
    )
    .unwrap();

    let test_path = dir.path().join("tests/example_flow.yaml");
    fs::write(
        &test_path,
        r#"name: starter flow transforms sample records
flow: example_flow
input: data/input.jsonl
expected_output: tests/expected_output.jsonl
assertions:
  - type: record_count
    count: 3
  - type: field_exists
    field: full_name
  - type: field_exists
    field: email
  - type: field_exists
    field: processed
  - type: field_value
    field: processed
    equals: true
  - type: field_not_exists
    field: name
  - type: field_not_exists
    field: age
"#,
    )
    .unwrap();

    assert!(test_path.exists(), "YAML test fixture should exist");
    assert!(
        expected_output_path.exists(),
        "expected output fixture should exist"
    );

    let repo_root = std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
        .parent()
        .and_then(std::path::Path::parent)
        .expect("crate should be inside the workspace");

    cargo_bin_cmd!("weavster")
        .current_dir(repo_root)
        .args(["--config", dir.path().to_str().unwrap(), "test"])
        .assert()
        .success()
        .stdout(
            predicate::str::contains("✓ starter flow transforms sample records")
                .and(predicate::str::contains("1 passed, 0 failed")),
        );
}
