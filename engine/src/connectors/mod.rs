//! Built-in connectors. `file` is the only one this phase; later connectors
//! (rest/blob/tcp/grpc/db) land here and register in [`crate::registry`].

pub mod file;
