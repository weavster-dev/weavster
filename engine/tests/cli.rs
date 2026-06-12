//! Run the compiled binary end-to-end against manifest-level failure cases —
//! these need no .wasm because manifest validation runs before module loading.
//! The full artifact-driven path lives in tests/artifact.rs.

use std::fs;
use std::path::PathBuf;
use std::process::{Command, Output};

const MIN_CONFIG: &str = "apiVersion: weavster/v0alpha2\nname: test\n";

/// Boot the engine against a staged artifact dir. A real `weavster.yaml` is
/// written into the dir (the boot anchor the engine requires) and `--artifact`
/// points at the same dir, so the test does not depend on the path convention.
fn run_engine(artifact_dir: &std::path::Path) -> Output {
    let config = artifact_dir.join("weavster.yaml");
    fs::write(&config, MIN_CONFIG).expect("write weavster.yaml");
    Command::new(env!("CARGO_BIN_EXE_weavster-engine"))
        .arg("-c")
        .arg(&config)
        .arg("--artifact")
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
fn missing_default_config_fails_with_a_clear_message() {
    // No args → the default mounted config path. Skip if it happens to exist on
    // this host, since the assertions assume it is absent.
    if std::path::Path::new("/etc/weavster/weavster.yaml").exists() {
        eprintln!("skipping: /etc/weavster/weavster.yaml exists on this host");
        return;
    }
    let output = Command::new(env!("CARGO_BIN_EXE_weavster-engine"))
        .output()
        .expect("run the weavster-engine binary");
    assert!(!output.status.success());
    let stderr = String::from_utf8_lossy(&output.stderr);
    assert!(stderr.contains("no weavster.yaml at"), "{stderr}");
    assert!(stderr.contains("/etc/weavster/weavster.yaml"), "{stderr}");
}

#[test]
fn help_flag_prints_usage_and_succeeds() {
    let output = Command::new(env!("CARGO_BIN_EXE_weavster-engine"))
        .arg("--help")
        .output()
        .expect("run the weavster-engine binary");
    assert!(output.status.success());
    let stdout = String::from_utf8_lossy(&output.stdout);
    assert!(stdout.contains("usage: weavster-engine"), "{stdout}");
}

#[test]
fn missing_artifact_dir_fails_with_a_clear_message() {
    // Config present (the boot anchor), but the artifact it points at is absent.
    let dir = temp_artifact("noart", GOLDEN_HEAD);
    fs::write(dir.join("weavster.yaml"), MIN_CONFIG).unwrap();
    let output = Command::new(env!("CARGO_BIN_EXE_weavster-engine"))
        .arg("-c")
        .arg(dir.join("weavster.yaml"))
        .arg("--artifact")
        .arg("/nonexistent/artifact")
        .output()
        .expect("run the weavster-engine binary");
    fs::remove_dir_all(&dir).ok();

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
    // Valid manifest and a matching input (so the source opens), but
    // flows/order.wasm does not exist.
    let dir = temp_artifact("noflow", GOLDEN_HEAD);
    fs::create_dir_all(dir.join("in")).unwrap();
    fs::write(dir.join("in/order.json"), "{}").unwrap();
    let output = run_engine(&dir);
    fs::remove_dir_all(&dir).ok();

    assert!(!output.status.success());
    let stderr = String::from_utf8_lossy(&output.stderr);
    assert!(stderr.contains("cannot load flow module"), "{stderr}");
}

#[test]
fn unknown_connector_type_fails_with_a_clear_error() {
    // The manifest is shape-valid; the registry rejects the connector type.
    let manifest =
        GOLDEN_HEAD.replace(r#"{ "type": "file", "glob""#, r#"{ "type": "rest", "glob""#);
    let dir = temp_artifact("badtype", &manifest);
    // Connectors are built before flow modules load, so no .wasm is needed —
    // the unknown type aborts startup first.
    let output = run_engine(&dir);
    fs::remove_dir_all(&dir).ok();

    assert!(!output.status.success());
    let stderr = String::from_utf8_lossy(&output.stderr);
    assert!(stderr.contains("unknown source type \"rest\""), "{stderr}");
}
