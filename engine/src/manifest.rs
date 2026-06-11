//! Artifact manifest loading (Engine Plan E3 slice 1).
//!
//! `manifest.json` is the CLI↔engine contract (`docs/ARTIFACT_SPEC.md`): the
//! engine reads it and nothing else from the user's project. Unknown
//! `manifestVersion` or `abiVersion` values are refused loudly rather than
//! risking garbage output from a contract we don't understand.

use anyhow::{Context, Result, bail};
use serde::Deserialize;
use std::path::Path;

/// The manifest file shape this engine understands.
pub const MANIFEST_VERSION: &str = "1";
/// The wasm host ABI this engine can drive (Javy stdin/stdout).
pub const ABI_VERSION: &str = "javy-1";

#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields, rename_all = "camelCase")]
pub struct Manifest {
    pub manifest_version: String,
    pub abi_version: String,
    pub pipelines: Vec<Pipeline>,
}

#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Pipeline {
    pub name: String,
    pub source: Source,
    /// Flow name; resolves by convention to `flows/<flow>.wasm`.
    pub flow: String,
    pub sink: Sink,
}

#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Source {
    pub r#type: String,
    pub glob: String,
    pub format: String,
}

#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Sink {
    pub r#type: String,
    pub path: String,
    pub format: String,
}

/// Parse and validate a manifest from JSON text.
pub fn parse(text: &str) -> Result<Manifest> {
    let manifest: Manifest = serde_json::from_str(text).context("manifest.json is not valid")?;

    if manifest.manifest_version != MANIFEST_VERSION {
        bail!(
            "unsupported manifestVersion \"{}\" — this engine understands \"{}\"; \
             recompile the artifact or upgrade the engine",
            manifest.manifest_version,
            MANIFEST_VERSION
        );
    }
    if manifest.abi_version != ABI_VERSION {
        bail!(
            "unsupported abiVersion \"{}\" — this engine drives \"{}\"; \
             recompile the artifact or upgrade the engine",
            manifest.abi_version,
            ABI_VERSION
        );
    }
    if manifest.pipelines.is_empty() {
        bail!("manifest has no pipelines");
    }
    for pipeline in &manifest.pipelines {
        // `file` is the only connector this phase; E4 turns this into a registry.
        if pipeline.source.r#type != "file" {
            bail!(
                "pipeline \"{}\": unknown source type \"{}\" (only \"file\" is supported)",
                pipeline.name,
                pipeline.source.r#type
            );
        }
        if pipeline.sink.r#type != "file" {
            bail!(
                "pipeline \"{}\": unknown sink type \"{}\" (only \"file\" is supported)",
                pipeline.name,
                pipeline.sink.r#type
            );
        }
        // Every path in the manifest resolves against the artifact root; an
        // absolute path or a `..` component would silently escape it.
        check_contained(&pipeline.name, "source glob", &pipeline.source.glob)?;
        check_contained(&pipeline.name, "sink path", &pipeline.sink.path)?;
        if pipeline.flow.is_empty() || pipeline.flow.contains(['/', '\\']) || pipeline.flow == ".."
        {
            bail!(
                "pipeline \"{}\": flow \"{}\" is not a plain name (it becomes flows/<flow>.wasm)",
                pipeline.name,
                pipeline.flow
            );
        }
    }
    Ok(manifest)
}

/// Refuse a path that is empty, absolute, or contains a `..` component —
/// each would resolve outside the artifact (connector) root.
fn check_contained(pipeline: &str, field: &str, path: &str) -> Result<()> {
    if path.is_empty() {
        bail!("pipeline \"{pipeline}\": {field} is empty");
    }
    let p = Path::new(path);
    if p.is_absolute() {
        bail!("pipeline \"{pipeline}\": {field} \"{path}\" must be relative to the artifact root");
    }
    if p.components()
        .any(|c| matches!(c, std::path::Component::ParentDir))
    {
        bail!("pipeline \"{pipeline}\": {field} \"{path}\" must not contain \"..\"");
    }
    Ok(())
}

/// Load `manifest.json` from an artifact directory.
pub fn load(artifact_dir: &Path) -> Result<Manifest> {
    let path = artifact_dir.join("manifest.json");
    let text = std::fs::read_to_string(&path)
        .with_context(|| format!("cannot read {}", path.display()))?;
    parse(&text).with_context(|| format!("invalid manifest at {}", path.display()))
}

#[cfg(test)]
mod tests {
    use super::*;

    const GOLDEN: &str = r#"{
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
    fn parses_the_golden_manifest() {
        let m = parse(GOLDEN).expect("golden manifest parses");
        assert_eq!(m.pipelines.len(), 1);
        assert_eq!(m.pipelines[0].flow, "order");
        assert_eq!(m.pipelines[0].source.glob, "in/*.json");
        assert_eq!(m.pipelines[0].sink.format, "json");
    }

    #[test]
    fn refuses_an_unknown_manifest_version() {
        let text = GOLDEN.replace("\"manifestVersion\": \"1\"", "\"manifestVersion\": \"99\"");
        let err = parse(&text).unwrap_err().to_string();
        assert!(err.contains("unsupported manifestVersion \"99\""), "{err}");
        assert!(err.contains("recompile"), "{err}");
    }

    #[test]
    fn refuses_an_unknown_abi_version() {
        let text = GOLDEN.replace("\"abiVersion\": \"javy-1\"", "\"abiVersion\": \"javy-9\"");
        let err = parse(&text).unwrap_err().to_string();
        assert!(err.contains("unsupported abiVersion \"javy-9\""), "{err}");
    }

    #[test]
    fn refuses_an_empty_pipeline_list() {
        let text = r#"{"manifestVersion":"1","abiVersion":"javy-1","pipelines":[]}"#;
        let err = parse(text).unwrap_err().to_string();
        assert!(err.contains("no pipelines"), "{err}");
    }

    #[test]
    fn refuses_malformed_json() {
        assert!(parse("{ not json").is_err());
    }

    #[test]
    fn refuses_an_unknown_connector_type() {
        let text = GOLDEN.replace(r#"{ "type": "file", "glob""#, r#"{ "type": "rest", "glob""#);
        let err = parse(&text).unwrap_err().to_string();
        assert!(err.contains("unknown source type \"rest\""), "{err}");
    }

    #[test]
    fn refuses_an_absolute_sink_path() {
        let text = GOLDEN.replace("out/order.json", "/etc/order.json");
        let err = parse(&text).unwrap_err().to_string();
        assert!(err.contains("must be relative"), "{err}");
    }

    #[test]
    fn refuses_a_parent_dir_component_in_the_glob() {
        let text = GOLDEN.replace("in/*.json", "../outside/*.json");
        let err = parse(&text).unwrap_err().to_string();
        assert!(err.contains("must not contain \"..\""), "{err}");
    }

    #[test]
    fn refuses_a_flow_name_with_a_path_separator() {
        let text = GOLDEN.replace("\"flow\": \"order\"", "\"flow\": \"../order\"");
        let err = parse(&text).unwrap_err().to_string();
        assert!(err.contains("not a plain name"), "{err}");
    }

    #[test]
    fn refuses_unknown_fields() {
        let text = GOLDEN.replace(
            "\"manifestVersion\"",
            "\"surprise\": 1, \"manifestVersion\"",
        );
        assert!(parse(&text).is_err());
    }

    #[test]
    fn load_reports_a_missing_file() {
        let err = load(Path::new("/nonexistent")).unwrap_err().to_string();
        assert!(err.contains("cannot read"), "{err}");
    }
}
