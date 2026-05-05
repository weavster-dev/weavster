//! Test runner command

use anyhow::{Context, Result, anyhow};
use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::time::Instant;
use walkdir::WalkDir;

use weavster_codegen::{CompileOptions, Compiler};
use weavster_core::config::Config;
use weavster_core::testing::{TestDefinition, TestExecutor, TestMode};

/// Run tests
pub async fn run(config_path: &str, pattern: Option<&str>, profile: Option<&str>) -> Result<()> {
    // 0. Load configuration
    let config = Config::load_with_profile(config_path, profile)
        .context("Failed to load configuration for testing")?;

    // 1. Discover test YAMLs in `tests/` directory
    let mut tests: Vec<TestDefinition> = Vec::new();

    let tests_dir = config.base_path.join("tests");
    if tests_dir.exists() && tests_dir.is_dir() {
        for entry_res in WalkDir::new(&tests_dir) {
            let entry = match entry_res {
                Ok(e) => e,
                Err(e) => {
                    tracing::warn!("Failed to access entry in tests directory: {}", e);
                    continue;
                }
            };
            if entry
                .path()
                .extension()
                .is_some_and(|ext| ext == "yaml" || ext == "yml")
            {
                let content = std::fs::read_to_string(entry.path())
                    .with_context(|| format!("Failed to read test file: {:?}", entry.path()))?;
                let mut test_def: TestDefinition = serde_yaml::from_str(&content)
                    .with_context(|| format!("Failed to parse test YAML: {:?}", entry.path()))?;
                normalize_test_paths(&mut test_def, &config.base_path);
                tests.push(test_def);
            }
        }
    } else {
        println!("No tests/ directory found.");
        return Ok(());
    }

    // 2. Filter tests based on the optional CLI pattern
    if let Some(p) = pattern {
        tracing::info!("Running tests matching: {}", p);
        tests.retain(|t| t.name.contains(p));
    } else {
        tracing::info!("Running all tests");
    }

    if tests.is_empty() {
        println!("No tests matched the criteria.");
        return Ok(());
    }

    // 3. Compile flows required by tests
    tracing::info!("Compiling flows for testing...");
    let mut flow_wasm_paths = HashMap::new();
    let options = CompileOptions {
        output_dir: config.base_path.join(".weavster/output"),
        cache_dir: config.base_path.join(".weavster/cache"),
        ..Default::default()
    };
    let compiler = Compiler::new(options);

    // Identify unique flows needed by the filtered tests
    let mut flows_to_compile = std::collections::HashSet::new();
    for test in &tests {
        flows_to_compile.insert(test.flow.clone());
    }

    for flow_name in flows_to_compile {
        tracing::info!("Compiling flow: {}", flow_name);
        let flow_config_path = config
            .base_path
            .join("flows")
            .join(format!("{}.yaml", flow_name));

        if !flow_config_path.exists() {
            return Err(anyhow!(
                "Flow configuration not found: {:?}",
                flow_config_path
            ));
        }

        let compile_ctx = compiler
            .compile_flow(&flow_config_path)
            .await
            .with_context(|| format!("Failed to compile flow: {}", flow_name))?;

        let wasm_cache_path = config
            .base_path
            .join(".weavster/cache")
            .join(format!("{}.wasm", compile_ctx.hash));
        flow_wasm_paths.insert(flow_name, wasm_cache_path);
    }

    // 4. Execute tests
    let executor = TestExecutor::new(TestMode::Unit, flow_wasm_paths);
    let mut passed = 0;
    let mut failed = 0;

    for test in tests {
        tracing::debug!("Running test: {}", test.name);
        let start = Instant::now();
        let result = executor.run_test(&test).await?;
        let duration = start.elapsed();

        if result.passed {
            println!("✓ {} ({:.2}s)", test.name, duration.as_secs_f64());
            passed += 1;
        } else {
            println!("✗ {} ({:.2}s)", test.name, duration.as_secs_f64());
            for failure in result.failures {
                println!("  - {}", failure);
            }
            if let Some(diff) = result.diff {
                println!("  Diff:\n{}", diff);
            }
            failed += 1;
        }
    }

    println!("\n{} passed, {} failed", passed, failed);

    // 5. Set appropriate exit code on failure
    if failed > 0 {
        return Err(anyhow!("{} tests failed", failed));
    }

    Ok(())
}

fn normalize_test_paths(test: &mut TestDefinition, base_path: &Path) {
    let input = resolve_project_path(base_path, &test.input);
    let expected_output = resolve_project_path(base_path, &test.expected_output);

    test.input = input;
    test.expected_output = expected_output;
}

fn resolve_project_path(base_path: &Path, path: &Path) -> PathBuf {
    if path.is_absolute() {
        path.to_path_buf()
    } else {
        base_path.join(path)
    }
}
