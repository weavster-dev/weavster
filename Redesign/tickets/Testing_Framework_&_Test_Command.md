# Testing Framework & Test Command

## Overview

Implement the testing framework that enables users to define and run tests for their flows. This is a critical differentiator from traditional ESB tools and essential for the dbt-like developer experience.

**Spec References:**
- `spec:3afb66cd-a148-48b8-8ce9-0bf5860c562d/e13dad9c-eda8-46d1-be4f-09ce88c29a23` - Component Architecture (Testing Framework)
- `spec:3afb66cd-a148-48b8-8ce9-0bf5860c562d/71f5f5ce-0ce5-44c1-826e-997166b1accd` - Flow 3 (Test a Flow)
- `spec:3afb66cd-a148-48b8-8ce9-0bf5860c562d/ae5101d9-bf0f-4073-b8a0-1576e4feea30` - Epic Brief (testing is critical)

## Scope

**In Scope:**
- Implement Testing Framework:
  - Test definition parsing (YAML format)
  - Hybrid test execution (in-memory for speed, file-based for integration)
  - Assertion engine (record_count, field_exists, field_not_exists, custom assertions)
  - Diff generation (expected vs actual)
  - Test result reporting (pass/fail with details)
  - Store test results in database
- Implement `weavster test` CLI command:
  - Run all tests or specific tests
  - `--verbose` flag for detailed output
  - Exit code 0 for pass, 1 for fail
  - Color-coded output (green ✓, red ✗)
- Update `weavster init` template with example test

**Out of Scope:**
- Property-based testing (post-MVP)
- Test coverage metrics (post-MVP)
- Test generation tools (post-MVP)
- Performance benchmarking (separate concern)

## Key Architectural Decisions

1. **Test Definition Format:** YAML with clear structure
   ```yaml
   name: test_customer_enrichment
   flow: customer_enrichment
   input: ./tests/fixtures/customer_input.jsonl
   expected_output: ./tests/fixtures/customer_expected.jsonl
   assertions:
     - record_count: 5
     - field_exists: full_name
     - field_not_exists: first_name
     - field_value:
         field: status
         equals: "active"
   ```

2. **Test Execution:** Hybrid approach
   - **Unit mode (in-memory):** Fast, focused on transform logic
     - Load fixtures into memory
     - Execute transforms in-memory
     - Compare results in-memory
     - No file I/O during test
   - **Integration mode (file-based):** Tests full pipeline
     - Write test input to temp file
     - Run flow normally (as if `weavster run`)
     - Read output file, compare to expected
     - Tests connectors, file I/O, full pipeline

3. **Assertion Types:**
   - `record_count`: Exact number of output records
   - `field_exists`: Field must be present in all records
   - `field_not_exists`: Field must not be present in any record
   - `field_value`: Field must have specific value
   - `custom`: Custom assertion logic (future)

4. **Diff Generation:** Clear, actionable diffs
   ```
   Test failed: test_customer_enrichment

   Record 3 mismatch:
   - Expected: {"full_name": "John Doe", "status": "active"}
   + Actual:   {"full_name": "JohnDoe", "status": "active"}

   Difference:
     full_name: "John Doe" → "JohnDoe" (missing space)
   ```

5. **Test Isolation:** Each test is independent
   - Tests don't share state
   - Tests can run in parallel (future optimization)
   - Tests use separate temp directories

## Implementation Details

### Test Definition Parsing

```rust
#[derive(Debug, Deserialize)]
pub struct TestDefinition {
    pub name: String,
    pub flow: String,
    pub input: PathBuf,
    pub expected_output: PathBuf,
    pub assertions: Vec<Assertion>,
}

#[derive(Debug, Deserialize)]
#[serde(tag = "type")]
pub enum Assertion {
    RecordCount { count: usize },
    FieldExists { field: String },
    FieldNotExists { field: String },
    FieldValue { field: String, equals: Value },
}
```

### Test Executor

```rust
pub struct TestExecutor {
    engine: TransformEngine,
    mode: TestMode,
}

pub enum TestMode {
    Unit,        // In-memory, fast
    Integration, // File-based, full pipeline
}

impl TestExecutor {
    pub async fn run_test(&self, test: &TestDefinition) -> TestResult {
        match self.mode {
            TestMode::Unit => self.run_unit_test(test).await,
            TestMode::Integration => self.run_integration_test(test).await,
        }
    }

    async fn run_unit_test(&self, test: &TestDefinition) -> TestResult {
        // Load fixtures
        let input = read_jsonl(&test.input)?;
        let expected = read_jsonl(&test.expected_output)?;

        // Execute transforms in-memory
        let mut actual = vec![];
        for record in input {
            if let Some(output) = self.engine.process_message(&test.flow, &record).await? {
                actual.push(output);
            }
        }

        // Compare and assert
        self.compare_and_assert(&expected, &actual, &test.assertions)
    }

    async fn run_integration_test(&self, test: &TestDefinition) -> TestResult {
        // Create temp directory
        let temp_dir = tempfile::tempdir()?;

        // Copy input to temp
        let temp_input = temp_dir.path().join("input.jsonl");
        fs::copy(&test.input, &temp_input)?;

        // Run flow (full pipeline)
        let output_path = temp_dir.path().join("output.jsonl");
        run_flow_with_output(&test.flow, &temp_input, &output_path).await?;

        // Load and compare
        let actual = read_jsonl(&output_path)?;
        let expected = read_jsonl(&test.expected_output)?;

        self.compare_and_assert(&expected, &actual, &test.assertions)
    }

    fn compare_and_assert(
        &self,
        expected: &[Value],
        actual: &[Value],
        assertions: &[Assertion],
    ) -> TestResult {
        let mut result = TestResult::new();

        // Check assertions
        for assertion in assertions {
            match assertion {
                Assertion::RecordCount { count } => {
                    if actual.len() != *count {
                        result.add_failure(format!(
                            "Expected {} records, got {}",
                            count, actual.len()
                        ));
                    }
                }
                Assertion::FieldExists { field } => {
                    for (i, record) in actual.iter().enumerate() {
                        if !record.get(field).is_some() {
                            result.add_failure(format!(
                                "Record {}: field '{}' not found",
                                i, field
                            ));
                        }
                    }
                }
                // ... other assertions
            }
        }

        // Generate diff if mismatch
        if expected != actual {
            result.diff = Some(generate_diff(expected, actual));
        }

        result
    }
}
```

### Test Command

```rust
pub async fn test_command(verbose: bool, test_name: Option<String>) -> Result<()> {
    // Load all test definitions
    let tests = load_tests("tests/")?;

    // Filter if specific test requested
    let tests = if let Some(name) = test_name {
        tests.into_iter().filter(|t| t.name == name).collect()
    } else {
        tests
    };

    // Run tests
    let executor = TestExecutor::new(TestMode::Unit)?;
    let mut results = vec![];

    for test in tests {
        let start = Instant::now();
        let result = executor.run_test(&test).await?;
        let duration = start.elapsed();

        results.push((test, result, duration));
    }

    // Report results
    let mut passed = 0;
    let mut failed = 0;

    for (test, result, duration) in results {
        if result.is_pass() {
            println!("✓ {} ({:.2}s)", test.name.green(), duration.as_secs_f64());
            passed += 1;
        } else {
            println!("✗ {} ({:.2}s)", test.name.red(), duration.as_secs_f64());
            if verbose {
                println!("{}", result.format_verbose());
            } else {
                println!("  {}", result.summary());
            }
            failed += 1;
        }
    }

    println!("\n{} passed, {} failed", passed, failed);

    if failed > 0 {
        std::process::exit(1);
    }

    Ok(())
}
```

## Acceptance Criteria

- [ ] Test definitions can be parsed from YAML
- [ ] Unit test mode executes tests in-memory
- [ ] Integration test mode executes full pipeline
- [ ] All assertion types work correctly
- [ ] Diff generation shows clear differences
- [ ] `weavster test` runs all tests
- [ ] `weavster test <name>` runs specific test
- [ ] `--verbose` flag shows detailed output
- [ ] Exit code is 0 for pass, 1 for fail
- [ ] Test results are stored in database
- [ ] Color-coded output (green ✓, red ✗)
- [ ] `weavster init` template includes example test
- [ ] Unit tests for test framework components
- [ ] Integration tests for test execution

## Testing Strategy

**Unit Tests:**
- Test definition parsing
- Assertion evaluation
- Diff generation
- Test result formatting

**Integration Tests:**
- Run unit test mode with sample fixtures
- Run integration test mode with sample fixtures
- Verify assertions catch failures
- Verify diff generation is accurate

**Meta-Tests:**
- Tests for the test framework itself
- Ensure test isolation
- Verify test result storage

## Dependencies

- **Depends on:** Transform Engine (Ticket 5) - for executing transforms
- **Depends on:** Config Layer (Ticket 1) - for loading flow configs

## Estimated Effort

3-4 days (Phase 5 of implementation plan)
