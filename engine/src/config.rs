//! Engine boot configuration (Engine Plan E5).
//!
//! The engine boots from a mounted `weavster.yaml` — the nginx/postgres
//! convention: a known default path, overridable with `-c/--config`. The
//! artifact (`weavster compile` output) is resolved **by convention** relative
//! to that config — `<config-dir>/target/artifact`, matching compile's default
//! output — and is overridable with `--artifact`.
//!
//! `weavster.yaml`'s content is the project switchboard, consumed at compile
//! time; for the engine it is the boot anchor whose *location* roots the
//! artifact. The `manifest.json` inside the artifact is the authoritative
//! contract the engine reads (see `manifest.rs`).

use anyhow::{Result, bail};
use std::path::{Path, PathBuf};

/// Default mounted config path (k8s ConfigMap / volume convention).
pub const DEFAULT_CONFIG: &str = "/etc/weavster/weavster.yaml";

/// The project file looked for when `-c` points at a directory.
pub const PROJECT_FILE: &str = "weavster.yaml";

pub const USAGE: &str = "\
usage: weavster-engine [-c|--config <weavster.yaml>] [--artifact <dir>]

  -c, --config <path>   project config to boot from
                        (default: /etc/weavster/weavster.yaml)
      --artifact <dir>  compiled artifact directory
                        (default: <config-dir>/target/artifact)
  -h, --help            show this help";

/// A resolved boot plan: the config to boot from and the artifact to run.
#[derive(Debug)]
pub struct Boot {
    pub config: PathBuf,
    pub artifact: PathBuf,
}

/// What the parsed arguments asked for.
#[derive(Debug)]
pub enum Cli {
    Run(Boot),
    Help,
}

/// Parse argv (excluding argv[0]) into a boot plan. The only filesystem touch
/// is `resolve`'s directory probe — and a `-c` path is treated as a project
/// directory only if it already exists as one at parse time; otherwise it is
/// taken as the config file. That file's existence is checked in `main`.
pub fn parse<I: IntoIterator<Item = String>>(args: I) -> Result<Cli> {
    let mut config: Option<PathBuf> = None;
    let mut artifact: Option<PathBuf> = None;

    let mut args = args.into_iter();
    while let Some(arg) = args.next() {
        match arg.as_str() {
            "-h" | "--help" => return Ok(Cli::Help),
            "-c" | "--config" => config = Some(take_path(&mut args, &arg)?),
            "--artifact" => artifact = Some(take_path(&mut args, &arg)?),
            other => bail!("unknown argument \"{other}\"\n\n{USAGE}"),
        }
    }

    let config = config.unwrap_or_else(|| PathBuf::from(DEFAULT_CONFIG));
    Ok(Cli::Run(resolve(config, artifact)))
}

/// Take the next argument as a flag's path value. A missing value — whether the
/// flag is last (`-c`) or followed by another option (`-c --artifact`) — is a
/// parse error, not a path that silently becomes a bogus file.
fn take_path<I: Iterator<Item = String>>(args: &mut I, flag: &str) -> Result<PathBuf> {
    match args.next() {
        Some(value) if !is_flag(&value) => Ok(PathBuf::from(value)),
        _ => bail!("{flag} needs a path"),
    }
}

/// Whether a token is one of our option flags (so it can't be a flag's value).
fn is_flag(token: &str) -> bool {
    matches!(token, "-h" | "--help" | "-c" | "--config" | "--artifact")
}

/// Resolve the `-c` path to a project file and an artifact directory. If it
/// points at an existing directory (`is_dir` is a filesystem check), treat it
/// as the project root and read `weavster.yaml` inside it (matching the CLI's
/// `resolveProjectFile`); otherwise — including a not-yet-created directory
/// path — it is taken as the config file itself. The artifact defaults to
/// `<project-dir>/target/artifact` — `weavster compile`'s default output —
/// unless `--artifact` overrode it.
fn resolve(config: PathBuf, artifact: Option<PathBuf>) -> Boot {
    let (config, project_dir) = if config.is_dir() {
        let file = config.join(PROJECT_FILE);
        (file, config)
    } else {
        let dir = config
            .parent()
            .filter(|p| !p.as_os_str().is_empty())
            .unwrap_or_else(|| Path::new("."))
            .to_path_buf();
        (config, dir)
    };
    let artifact = artifact.unwrap_or_else(|| project_dir.join("target/artifact"));
    Boot { config, artifact }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn parse_run(args: &[&str]) -> Boot {
        match parse(args.iter().map(|s| s.to_string())) {
            Ok(Cli::Run(boot)) => boot,
            other => panic!("expected a run plan, got {}", describe(&other)),
        }
    }

    fn describe(cli: &Result<Cli>) -> &'static str {
        match cli {
            Ok(Cli::Run(_)) => "Run",
            Ok(Cli::Help) => "Help",
            Err(_) => "Err",
        }
    }

    #[test]
    fn no_args_uses_the_default_config_and_derived_artifact() {
        let boot = parse_run(&[]);
        assert_eq!(boot.config, Path::new(DEFAULT_CONFIG));
        assert_eq!(boot.artifact, Path::new("/etc/weavster/target/artifact"));
    }

    #[test]
    fn config_override_derives_the_artifact_next_to_it() {
        let boot = parse_run(&["-c", "/run/project/weavster.yaml"]);
        assert_eq!(boot.config, Path::new("/run/project/weavster.yaml"));
        assert_eq!(boot.artifact, Path::new("/run/project/target/artifact"));
    }

    #[test]
    fn long_config_flag_is_accepted() {
        let boot = parse_run(&["--config", "./weavster.yaml"]);
        assert_eq!(boot.config, Path::new("./weavster.yaml"));
        // "./weavster.yaml"'s parent is ".", so the artifact sits under the cwd.
        assert_eq!(boot.artifact, Path::new("./target/artifact"));
    }

    #[test]
    fn a_bare_filename_falls_back_to_the_cwd() {
        // "weavster.yaml" with no prefix has an empty parent; resolve falls back
        // to "." so the artifact is "./target/artifact".
        let boot = parse_run(&["-c", "weavster.yaml"]);
        assert_eq!(boot.config, Path::new("weavster.yaml"));
        assert_eq!(boot.artifact, Path::new("./target/artifact"));
    }

    #[test]
    fn a_config_directory_resolves_to_the_project_file_inside_it() {
        // `-c <dir>` mirrors the CLI: read <dir>/weavster.yaml, artifact under
        // <dir>/target/artifact. Needs a real directory (the FS probe).
        let dir = std::env::temp_dir().join(format!("wv-cfg-{}", std::process::id()));
        std::fs::create_dir_all(&dir).unwrap();
        let boot = parse_run(&["-c", dir.to_str().unwrap()]);
        assert_eq!(boot.config, dir.join("weavster.yaml"));
        assert_eq!(boot.artifact, dir.join("target/artifact"));
        std::fs::remove_dir_all(&dir).ok();
    }

    #[test]
    fn artifact_override_wins_over_the_convention() {
        let boot = parse_run(&[
            "-c",
            "/etc/weavster/weavster.yaml",
            "--artifact",
            "/data/art",
        ]);
        assert_eq!(boot.artifact, Path::new("/data/art"));
    }

    #[test]
    fn help_flag_short_and_long() {
        assert!(matches!(parse(["-h".to_string()]).unwrap(), Cli::Help));
        assert!(matches!(parse(["--help".to_string()]).unwrap(), Cli::Help));
    }

    #[test]
    fn unknown_argument_is_rejected_with_usage() {
        let err = parse(["--nope".to_string()]).unwrap_err().to_string();
        assert!(err.contains("unknown argument \"--nope\""), "{err}");
        assert!(err.contains("usage:"), "{err}");
    }

    #[test]
    fn a_flag_without_its_value_is_rejected() {
        let err = parse(["-c".to_string()]).unwrap_err().to_string();
        assert!(err.contains("needs a path"), "{err}");
        let err = parse(["--artifact".to_string()]).unwrap_err().to_string();
        assert!(err.contains("needs a path"), "{err}");
    }

    #[test]
    fn a_flag_value_that_is_another_option_is_rejected() {
        // `-c --artifact` is a missing config value, not a config path of
        // "--artifact" that later fails as a bogus file.
        let err = parse(["-c".to_string(), "--artifact".to_string()])
            .unwrap_err()
            .to_string();
        assert!(err.contains("-c needs a path"), "{err}");
    }
}
