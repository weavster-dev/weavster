//! End-to-end tests against a real compiled artifact (`weavster compile`
//! output). These need the TS toolchain's build product, which `cargo test`
//! can't produce — so they self-skip unless the golden artifact exists at
//! `examples/golden-path/target/artifact` (or `WEAVSTER_ARTIFACT` points
//! elsewhere). Build it with: `pnpm --filter @weavster/cli dev compile
//! examples/golden-path` from the repo root.

use std::fs;
use std::path::{Path, PathBuf};
use std::process::{Command, Output};

fn golden_artifact() -> Option<PathBuf> {
    let dir = std::env::var("WEAVSTER_ARTIFACT")
        .map(PathBuf::from)
        .unwrap_or_else(|_| {
            Path::new(env!("CARGO_MANIFEST_DIR")).join("../examples/golden-path/target/artifact")
        });
    if dir.join("manifest.json").exists() && dir.join("flows/order.wasm").exists() {
        Some(dir)
    } else {
        eprintln!(
            "skipping: no compiled artifact at {} (run weavster compile)",
            dir.display()
        );
        None
    }
}

const ORDER_DOC: &str = r#"{ "id": "a1", "first": "Ada", "last": "Lovelace", "status": "new" }"#;

/// Stage a runnable copy: manifest (with the given source glob) + flows/ +
/// the provided input files under in/.
fn stage(name: &str, artifact: &Path, glob: &str, inputs: &[(&str, &str)]) -> PathBuf {
    let dir = std::env::temp_dir().join(format!("wv-artifact-{}-{}", name, std::process::id()));
    fs::remove_dir_all(&dir).ok();
    fs::create_dir_all(dir.join("flows")).unwrap();
    fs::create_dir_all(dir.join("in")).unwrap();
    fs::copy(
        artifact.join("flows/order.wasm"),
        dir.join("flows/order.wasm"),
    )
    .unwrap();

    let manifest = fs::read_to_string(artifact.join("manifest.json"))
        .unwrap()
        .replace("in/order.json", glob);
    fs::write(dir.join("manifest.json"), manifest).unwrap();

    for (file, content) in inputs {
        fs::write(dir.join("in").join(file), content).unwrap();
    }
    dir
}

fn run_engine(artifact_dir: &Path) -> Output {
    Command::new(env!("CARGO_BIN_EXE_weavster-engine"))
        .arg(artifact_dir)
        .output()
        .expect("run the weavster-engine binary")
}

#[test]
fn runs_the_golden_pipeline_end_to_end() {
    let Some(artifact) = golden_artifact() else {
        return;
    };
    let dir = stage(
        "golden",
        &artifact,
        "in/order.json",
        &[("order.json", ORDER_DOC)],
    );

    let output = run_engine(&dir);
    let stderr = String::from_utf8_lossy(&output.stderr);
    assert!(output.status.success(), "{stderr}");

    let written = fs::read_to_string(dir.join("out/order.json")).unwrap();
    assert!(written.contains("\"id\": \"A1\""), "{written}");
    assert!(written.contains("\"name\": \"Ada Lovelace\""), "{written}");
    assert!(written.contains("\"priority\": \"high\""), "{written}");
    assert!(written.contains("\"initials\": \"AL\""), "{written}");

    fs::remove_dir_all(&dir).ok();
}

#[test]
fn processes_glob_matches_in_input_order_with_structured_logs() {
    let Some(artifact) = golden_artifact() else {
        return;
    };
    let dir = stage(
        "order",
        &artifact,
        "in/*.json",
        &[
            ("a.json", ORDER_DOC),
            ("b.json", ORDER_DOC),
            ("c.json", ORDER_DOC),
        ],
    );

    let output = run_engine(&dir);
    let stderr = String::from_utf8_lossy(&output.stderr);
    assert!(output.status.success(), "{stderr}");
    assert!(
        stderr.contains("1/1 pipelines ran (3 documents)"),
        "{stderr}"
    );

    // Structured log lines carry pipeline/document fields, in input order.
    let docs: Vec<u64> = stderr
        .lines()
        .filter_map(|line| serde_json::from_str::<serde_json::Value>(line).ok())
        .filter(|v| v["event"] == "document")
        .map(|v| v["document"].as_u64().unwrap())
        .collect();
    assert_eq!(docs, [1, 2, 3], "{stderr}");

    fs::remove_dir_all(&dir).ok();
}

#[test]
fn a_poison_document_fails_the_bounded_run_with_stage() {
    let Some(artifact) = golden_artifact() else {
        return;
    };
    let dir = stage(
        "poison",
        &artifact,
        "in/*.json",
        &[("a.json", ORDER_DOC), ("b.json", "{ not json")],
    );

    let output = run_engine(&dir);
    let stderr = String::from_utf8_lossy(&output.stderr);
    assert!(!output.status.success());
    // The failure is scoped: pipeline + document + stage, in the structured log.
    let error_line = stderr
        .lines()
        .filter_map(|line| serde_json::from_str::<serde_json::Value>(line).ok())
        .find(|v| v["level"] == "error")
        .unwrap_or_else(|| panic!("no structured error line in: {stderr}"));
    assert_eq!(error_line["pipeline"], "order");
    assert_eq!(error_line["document"], 2);
    assert_eq!(error_line["stage"], "parse");

    fs::remove_dir_all(&dir).ok();
}
