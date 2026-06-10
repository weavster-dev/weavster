//! Run the compiled binary end-to-end against manifest-level failure cases —
//! these need no .wasm because manifest validation runs before module loading.
//! The full artifact-driven path lives in tests/artifact.rs.

use std::fs;
use std::path::PathBuf;
use std::process::{Command, Output};

fn run_engine(artifact_dir: &std::path::Path) -> Output {
    Command::new(env!("CARGO_BIN_EXE_weavster-engine"))
        .arg(artifact_dir)
        .output()
        .expect("run the weavster-engine binary")
}

fn temp_artifact(name: &str, manifest: &str) -> PathBuf {
    let dir = std::env::temp_dir().join(format!("wv-engine-{}-{}", name, std::process::id()));
    fs::create_dir_all(&dir).expect("create temp artifact dir");
    fs::write(dir.join("manifest.json"), manifest).expect("write manifest");
    dir
}

const GOLDEN_HEAD: &str = r#"{
  "manifestVersion": "1",
  "abiVersion": "javy-1",
  "pipelines": [
    {
      "name": "orders",
      "source": { "type": "file", "glob": "in/*.json", "format": "json" },
      "flow": "order",
      "sink": { "type": "file", "path": "out/order.json", "format": "json" }
    }
  ]
}"#;

#[test]
fn missing_artifact_dir_fails_with_a_clear_message() {
    let output = run_engine(std::path::Path::new("/nonexistent/artifact"));
    assert!(!output.status.success());
    let stderr = String::from_utf8_lossy(&output.stderr);
    assert!(stderr.contains("cannot read"), "{stderr}");
    assert!(stderr.contains("manifest.json"), "{stderr}");
}

#[test]
fn mismatched_abi_version_fails_fast() {
    let manifest = GOLDEN_HEAD.replace("javy-1", "javy-99");
    let dir = temp_artifact("abi", &manifest);
    let output = run_engine(&dir);
    fs::remove_dir_all(&dir).ok();

    assert!(!output.status.success());
    let stderr = String::from_utf8_lossy(&output.stderr);
    assert!(
        stderr.contains("unsupported abiVersion \"javy-99\""),
        "{stderr}"
    );
    assert!(stderr.contains("recompile"), "{stderr}");
}

#[test]
fn unknown_manifest_version_fails_fast() {
    let manifest = GOLDEN_HEAD.replace("\"manifestVersion\": \"1\"", "\"manifestVersion\": \"2\"");
    let dir = temp_artifact("mv", &manifest);
    let output = run_engine(&dir);
    fs::remove_dir_all(&dir).ok();

    assert!(!output.status.success());
    let stderr = String::from_utf8_lossy(&output.stderr);
    assert!(
        stderr.contains("unsupported manifestVersion \"2\""),
        "{stderr}"
    );
}

#[test]
fn missing_flow_module_fails_at_startup() {
    // Valid manifest, but flows/order.wasm does not exist.
    let dir = temp_artifact("noflow", GOLDEN_HEAD);
    let output = run_engine(&dir);
    fs::remove_dir_all(&dir).ok();

    assert!(!output.status.success());
    let stderr = String::from_utf8_lossy(&output.stderr);
    assert!(stderr.contains("cannot load flow module"), "{stderr}");
}
