//! Connector traits (Engine Plan E4): the seam between the engine's run loop
//! and the outside world. A `Source` yields documents in order; a `Sink`
//! writes them. `file` is the only connector this phase; later connectors
//! (rest/blob/tcp/grpc/db) implement the same traits and register in
//! `registry`, so they are additive — no run-loop change.
//!
//! The traits are async because every connector beyond `file` is async I/O;
//! landing the async shape now means those connectors slot in without a
//! breaking trait change. The transform itself stays synchronous (it runs in
//! `spawn_blocking` off the async worker).

use anyhow::Result;
use async_trait::async_trait;

/// One document a source yields: its text payload plus an origin label used in
/// logs and error messages (e.g. the file path it came from, or a URL). A
/// `String` keeps the type connector-agnostic — not every origin is a path.
pub struct SourceDoc {
    pub origin: String,
    pub payload: String,
}

/// A stream of documents, yielded in order, one in flight at a time.
#[async_trait]
pub trait Source: Send {
    /// The next document, or `None` once the source is exhausted.
    async fn next(&mut self) -> Result<Option<SourceDoc>>;
}

/// A destination for transformed documents.
#[async_trait]
pub trait Sink: Send {
    /// Write one serialized document.
    async fn write(&mut self, payload: &str) -> Result<()>;
}
