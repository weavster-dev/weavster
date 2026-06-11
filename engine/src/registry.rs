//! Connector registry (Engine Plan E4): maps a manifest connector `type` to a
//! concrete [`Source`]/[`Sink`]. This is the single place that knows which
//! connector types exist, so adding one is a new match arm here plus its
//! module under `connectors/` — the run loop never changes. The manifest specs
//! ([`SourceSpec`]/[`SinkSpec`]) are still file-shaped (`glob`/`path`), so a
//! non-`file` connector also turns those flat structs into a `#[serde(tag =
//! "type")]` enum — do that rather than bolting on `Option<_>` fields.

use crate::connector::{Sink, Source};
use crate::connectors::file::{FileSink, FileSource};
use crate::manifest::{SinkSpec, SourceSpec};
use anyhow::{Result, bail};
use std::path::Path;

/// Build the source for a pipeline, resolving paths against the connector root.
pub fn build_source(root: &Path, spec: &SourceSpec) -> Result<Box<dyn Source>> {
    match spec.r#type.as_str() {
        "file" => Ok(Box::new(FileSource::new(root, &spec.glob)?)),
        other => bail!("unknown source type \"{other}\" (only \"file\" is supported)"),
    }
}

/// Build the sink for a pipeline, resolving paths against the connector root.
pub fn build_sink(root: &Path, spec: &SinkSpec) -> Result<Box<dyn Sink>> {
    match spec.r#type.as_str() {
        "file" => Ok(Box::new(FileSink::new(root, &spec.path)?)),
        other => bail!("unknown sink type \"{other}\" (only \"file\" is supported)"),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::manifest::{SinkSpec, SourceSpec};

    #[test]
    fn rejects_an_unknown_source_type() {
        let spec = SourceSpec {
            r#type: "rest".into(),
            glob: "in/*.json".into(),
            format: "json".into(),
        };
        let err = build_source(Path::new("/tmp"), &spec)
            .err()
            .unwrap()
            .to_string();
        assert!(err.contains("unknown source type \"rest\""), "{err}");
    }

    #[test]
    fn rejects_an_unknown_sink_type() {
        let spec = SinkSpec {
            r#type: "blob".into(),
            path: "out/x.json".into(),
            format: "json".into(),
        };
        let err = build_sink(Path::new("/tmp"), &spec)
            .err()
            .unwrap()
            .to_string();
        assert!(err.contains("unknown sink type \"blob\""), "{err}");
    }
}
