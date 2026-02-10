use assert_cmd::cargo::cargo_bin_cmd;

#[test]
fn test_init_and_run_once() {
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

    // Run --once
    cargo_bin_cmd!("weavster")
        .args(["--config", dir.path().to_str().unwrap(), "run", "--once"])
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

    // Third record: Carol
    assert_eq!(lines[2]["full_name"], "Carol Williams");
    assert_eq!(lines[2]["email"], "carol@example.com");
    assert_eq!(lines[2]["processed"], true);
}
