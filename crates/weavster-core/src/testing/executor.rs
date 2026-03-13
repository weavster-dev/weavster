use super::assertions::generate_diff;
use super::models::{Assertion, TestDefinition};
use crate::error::Result;
use serde::{Deserialize, Serialize};
use serde_json::Value;

/// Evaluation strategy logic
#[derive(Debug, Clone, Copy)]
pub enum TestMode {
    /// In-memory execution using pure wasm abstractions
    Unit,
    /// Full end-to-end execution mocking real IO systems
    Integration,
}

/// The result returned after assessing a test block
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TestResult {
    /// Did the test pass all constraints?
    pub passed: bool,
    /// Log vector on missed constraints
    pub failures: Vec<String>,
    /// Optional generated diff
    pub diff: Option<String>,
}

impl Default for TestResult {
    fn default() -> Self {
        Self::new()
    }
}

impl TestResult {
    /// Default empty success result
    pub fn new() -> Self {
        Self {
            passed: true,
            failures: Vec::new(),
            diff: None,
        }
    }

    /// Push an error into the internal results
    pub fn add_failure(&mut self, reason: String) {
        self.passed = false;
        self.failures.push(reason);
    }
}

/// A pipeline execution mechanism
pub struct TestExecutor {
    /// Tracks if we execute WASM modules natively or spawn up server systems.
    pub mode: TestMode,
}

impl TestExecutor {
    /// Spin up an instance
    pub fn new(mode: TestMode) -> Self {
        Self { mode }
    }

    /// Entry point for evaluating tests
    pub async fn run_test(&self, test: &TestDefinition) -> Result<TestResult> {
        match self.mode {
            TestMode::Unit => self.run_unit_test(test),
            TestMode::Integration => self.run_integration_test(test),
        }
    }

    fn run_unit_test(&self, test: &TestDefinition) -> Result<TestResult> {
        // MOCK IMPLEMENTATION FOR MVP
        // TODO: Actually construct WASM engine runtime and feed `.jsonl` directly

        let expected = read_jsonl(&test.expected_output)?;
        let actual = read_jsonl(&test.input)?; // temporary mock

        Ok(self.compare_and_assert(&expected, &actual, &test.assertions))
    }

    fn run_integration_test(&self, test: &TestDefinition) -> Result<TestResult> {
        // MOCK IMPLEMENTATION FOR MVP
        // TODO: actually orchestrate the file watching system on the temp directory mapped to integration configs

        let expected = read_jsonl(&test.expected_output)?;
        let actual = read_jsonl(&test.expected_output)?; // temporary mock returning expected

        Ok(self.compare_and_assert(&expected, &actual, &test.assertions))
    }
    fn compare_and_assert(
        &self,
        expected: &[Value],
        actual: &[Value],
        assertions: &[Assertion],
    ) -> TestResult {
        let mut result = TestResult::new();

        // Check configured assertions
        for assertion in assertions {
            if let Err(e) = assertion.evaluate(actual) {
                result.add_failure(e.to_string());
            }
        }

        // Generate JSON diff if structural outputs mismatch completely
        if expected != actual {
            result.add_failure("Actual JSON output does not match expected.".into());
            result.diff = Some(generate_diff(expected, actual));
        }

        result
    }
}

fn read_jsonl(path: &std::path::Path) -> Result<Vec<Value>> {
    use std::fs::File;
    use std::io::{BufRead, BufReader};

    let file = File::open(path)?;
    let reader = BufReader::new(file);

    let mut records = Vec::new();
    for line in reader.lines() {
        let line = line?;
        if line.trim().is_empty() {
            continue;
        }
        let val: Value = serde_json::from_str(&line)?;
        records.push(val);
    }

    Ok(records)
}
