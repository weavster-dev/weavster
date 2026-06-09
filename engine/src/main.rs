//! Weavster engine — the thin Rust production runtime (RFC 0003).
//!
//! E0 stands the crate up inside the (otherwise TS) monorepo. The engine
//! itself — manifest loading, the wasmtime host over the Javy ABI, and the
//! per-pipeline run loop — lands in later milestones (E3+). See
//! `docs/ENGINE_PLAN.md`.

fn banner() -> String {
    format!(
        "weavster-engine {} — not yet implemented (see docs/ENGINE_PLAN.md)",
        env!("CARGO_PKG_VERSION")
    )
}

fn main() {
    println!("{}", banner());
}

#[cfg(test)]
mod tests {
    use super::banner;

    #[test]
    fn banner_names_the_engine_and_version() {
        let b = banner();
        assert!(b.contains("weavster-engine"));
        assert!(b.contains(env!("CARGO_PKG_VERSION")));
    }
}
