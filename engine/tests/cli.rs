//! Run the compiled binary end-to-end. This exercises `main` (which a unit test
//! cannot), so coverage reflects the whole crate. `CARGO_BIN_EXE_<name>` is set
//! by Cargo for integration tests — no extra dependency.

use std::process::Command;

#[test]
fn prints_the_banner() {
    let output = Command::new(env!("CARGO_BIN_EXE_weavster-engine"))
        .output()
        .expect("run the weavster-engine binary");

    assert!(output.status.success());
    let stdout = String::from_utf8(output.stdout).expect("utf8 stdout");
    assert!(stdout.contains("weavster-engine"));
    assert!(stdout.contains(env!("CARGO_PKG_VERSION")));
}
