//! The `file` connector (Engine Plan E4): a glob source and a path sink, both
//! resolved against the connector root (the artifact directory).

use crate::connector::{Sink, Source, SourceDoc};
use anyhow::{Context, Result, bail};
use async_trait::async_trait;
use std::collections::VecDeque;
use std::path::{Path, PathBuf};

/// Reads each file a glob matches, in sorted (input) order. One file is one
/// document this phase; multi-record files are a later expansion.
pub struct FileSource {
    remaining: VecDeque<PathBuf>,
}

impl FileSource {
    /// Resolve `glob` against `root` now, so an unreadable or empty pattern
    /// fails at startup rather than mid-run. The manifest gate
    /// (`manifest::check_contained`) guarantees `glob` is relative and free of
    /// `..`, so `root.join` stays inside the connector root.
    pub fn new(root: &Path, glob: &str) -> Result<Self> {
        let joined = root.join(glob);
        let pattern = joined.to_str().context("glob pattern is not valid UTF-8")?;
        let mut paths: Vec<PathBuf> = glob::glob(pattern)
            .context("invalid glob pattern")?
            .collect::<std::result::Result<_, _>>()
            .context("cannot read a glob match")?;
        paths.sort();
        if paths.is_empty() {
            bail!("glob \"{glob}\" matched no files");
        }
        Ok(Self {
            remaining: paths.into(),
        })
    }
}

#[async_trait]
impl Source for FileSource {
    async fn next(&mut self) -> Result<Option<SourceDoc>> {
        let Some(path) = self.remaining.pop_front() else {
            return Ok(None);
        };
        let payload = tokio::fs::read_to_string(&path)
            .await
            .with_context(|| format!("cannot read {}", path.display()))?;
        Ok(Some(SourceDoc {
            origin: path.display().to_string(),
            payload,
        }))
    }
}

/// Writes to a single path, overwriting per document (last write wins) — the
/// TS file connector's semantics. Per-document naming for multi-match globs is
/// a later decision.
pub struct FileSink {
    path: PathBuf,
}

impl FileSink {
    /// Create the destination's parent directory now (once), so per-document
    /// writes don't each re-issue a `create_dir_all`. The manifest gate keeps
    /// `path` inside the connector root.
    pub fn new(root: &Path, path: &str) -> Result<Self> {
        let path = root.join(path);
        if let Some(parent) = path.parent() {
            std::fs::create_dir_all(parent)
                .with_context(|| format!("cannot create {}", parent.display()))?;
        }
        Ok(Self { path })
    }
}

#[async_trait]
impl Sink for FileSink {
    async fn write(&mut self, payload: &str) -> Result<()> {
        tokio::fs::write(&self.path, payload)
            .await
            .with_context(|| format!("cannot write {}", self.path.display()))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn temp(name: &str) -> PathBuf {
        let dir = std::env::temp_dir().join(format!("wv-file-{name}-{}", std::process::id()));
        std::fs::create_dir_all(&dir).unwrap();
        dir
    }

    /// Drive an async test body on a fresh current-thread runtime (the `macros`
    /// feature is off, so there's no `#[tokio::test]`).
    fn block_on<F: std::future::Future>(future: F) -> F::Output {
        tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
            .unwrap()
            .block_on(future)
    }

    #[test]
    fn source_yields_each_glob_match_in_sorted_order() {
        let dir = temp("src");
        std::fs::create_dir_all(dir.join("in")).unwrap();
        std::fs::write(dir.join("in/b.json"), "B").unwrap();
        std::fs::write(dir.join("in/a.json"), "A").unwrap();

        block_on(async {
            let mut source = FileSource::new(&dir, "in/*.json").unwrap();
            let first = source.next().await.unwrap().unwrap();
            let second = source.next().await.unwrap().unwrap();
            assert_eq!(first.payload, "A");
            assert_eq!(second.payload, "B");
            assert!(source.next().await.unwrap().is_none());
        });

        std::fs::remove_dir_all(&dir).ok();
    }

    #[test]
    fn source_rejects_an_empty_match() {
        let dir = temp("empty");
        let err = FileSource::new(&dir, "in/*.json")
            .err()
            .unwrap()
            .to_string();
        assert!(err.contains("matched no files"), "{err}");
        std::fs::remove_dir_all(&dir).ok();
    }

    #[test]
    fn sink_writes_the_payload_creating_parents() {
        let dir = temp("sink");
        block_on(async {
            let mut sink = FileSink::new(&dir, "out/x.json").unwrap();
            sink.write("hello").await.unwrap();
        });
        assert_eq!(
            std::fs::read_to_string(dir.join("out/x.json")).unwrap(),
            "hello"
        );
        std::fs::remove_dir_all(&dir).ok();
    }
}
