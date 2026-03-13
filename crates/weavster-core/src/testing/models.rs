use serde::{Deserialize, Serialize};
use std::path::PathBuf;

/// Defines a test case for a Weavster flow
#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct TestDefinition {
    /// Name of the test
    pub name: String,

    /// The name of the flow to test (must exist in the project)
    pub flow: String,

    /// Path to the newline-delimited JSON input fixture
    pub input: PathBuf,

    /// Path to the newline-delimited JSON expected output fixture
    pub expected_output: PathBuf,

    /// List of assertions to enforce on the flow's output
    #[serde(default)]
    pub assertions: Vec<Assertion>,
}

/// A specific invariant that must hold true for the flow's output
#[derive(Debug, Clone, PartialEq, Deserialize, Serialize)]
#[serde(rename_all = "snake_case", tag = "type")]
pub enum Assertion {
    /// Asserts that the exact number of output records matches
    RecordCount {
        /// Asserted total output lines
        count: usize,
    },

    /// Asserts that a specified field exists in EVERY output record
    FieldExists {
        /// Assert field presence
        field: String,
    },

    /// Asserts that a specified field does NOT exist in ANY output record
    FieldNotExists {
        /// Assert field absence
        field: String,
    },

    /// Asserts that a specified field has a specific JSON value in EVERY output record
    FieldValue {
        /// The specific field key
        field: String,
        /// The parsed equality as a serde_json::Value (may be object, array, string, number, bool, or null)
        equals: serde_json::Value,
    },
}
