use super::models::Assertion;
use serde_json::Value;

/// Error type describing why a specific assertion failed
#[derive(Debug, Clone, PartialEq, Eq, thiserror::Error)]
pub enum AssertionError {
    /// The number of records did not match expected count
    #[error("Expected {expected} records, got {actual}")]
    RecordCountMismatch {
        /// Expected amount
        expected: usize,
        /// Actual amount
        actual: usize,
    },

    /// The specific field was missing entirely across records
    #[error("Record {record_index}: expected field '{field}' to exist")]
    FieldMissing {
        /// Record index
        record_index: usize,
        /// The missing field
        field: String,
    },

    /// The field existed where we explicitly asserted it should not
    #[error("Record {record_index}: expected field '{field}' to NOT exist")]
    FieldExistsWhenShouldNot {
        /// Record index
        record_index: usize,
        /// The unexpected field
        field: String,
    },

    /// The evaluated value did not match our strict assertion equality
    #[error("Record {record_index}: field '{field}' expected value {expected}, got {actual}")]
    FieldValueMismatch {
        /// Record index
        record_index: usize,
        /// Examined field
        field: String,
        /// Expected raw value
        expected: Value,
        /// Actual raw value
        actual: Value,
    },
}

impl Assertion {
    /// Evaluates this assertion against a sequence of materialized JSON output records from a flow.
    pub fn evaluate(&self, actual_outputs: &[Value]) -> Result<(), AssertionError> {
        match self {
            Assertion::RecordCount { count } => {
                if actual_outputs.len() != *count {
                    return Err(AssertionError::RecordCountMismatch {
                        expected: *count,
                        actual: actual_outputs.len(),
                    });
                }
            }
            Assertion::FieldExists { field } => {
                for (i, record) in actual_outputs.iter().enumerate() {
                    if record.get(field).is_none() {
                        return Err(AssertionError::FieldMissing {
                            record_index: i,
                            field: field.clone(),
                        });
                    }
                }
            }
            Assertion::FieldNotExists { field } => {
                for (i, record) in actual_outputs.iter().enumerate() {
                    if record.get(field).is_some() {
                        return Err(AssertionError::FieldExistsWhenShouldNot {
                            record_index: i,
                            field: field.clone(),
                        });
                    }
                }
            }
            Assertion::FieldValue { field, equals } => {
                for (i, record) in actual_outputs.iter().enumerate() {
                    match record.get(field) {
                        None => {
                            return Err(AssertionError::FieldMissing {
                                record_index: i,
                                field: field.clone(),
                            });
                        }
                        Some(val) => {
                            if val != equals {
                                return Err(AssertionError::FieldValueMismatch {
                                    record_index: i,
                                    field: field.clone(),
                                    expected: equals.clone(),
                                    actual: val.clone(),
                                });
                            }
                        }
                    }
                }
            }
        }
        Ok(())
    }
}

/// Helper method to generate a rudimentary diff string showing JSON-level output mismatches
pub fn generate_diff(expected: &[Value], actual: &[Value]) -> String {
    use std::cmp::max;
    let mut diff_str = String::new();

    let max_len = max(expected.len(), actual.len());

    for i in 0..max_len {
        let exp = expected.get(i);
        let act = actual.get(i);

        if exp != act {
            diff_str.push_str(&format!("Record {} mismatch:\n", i));
            match (exp, act) {
                (Some(e), Some(a)) => {
                    diff_str.push_str(&format!(
                        "- Expected: {}\n",
                        serde_json::to_string(e).unwrap_or_default()
                    ));
                    diff_str.push_str(&format!(
                        "+ Actual:   {}\n",
                        serde_json::to_string(a).unwrap_or_default()
                    ));
                }
                (Some(e), None) => {
                    diff_str.push_str(&format!(
                        "- Expected: {}\n",
                        serde_json::to_string(e).unwrap_or_default()
                    ));
                    diff_str.push_str("+ Actual:   <Missing Record>\n");
                }
                (None, Some(a)) => {
                    diff_str.push_str("- Expected: <Missing Record>\n");
                    diff_str.push_str(&format!(
                        "+ Actual:   {}\n",
                        serde_json::to_string(a).unwrap_or_default()
                    ));
                }
                (None, None) => unreachable!("exp != act ensures (None, None) is impossible"),
            }
            diff_str.push('\n');
        }
    }

    diff_str
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn test_record_count_assertion() {
        let actual = vec![json!({"a": 1}), json!({"b": 2})];

        let pass = Assertion::RecordCount { count: 2 };
        assert!(pass.evaluate(&actual).is_ok());

        let fail = Assertion::RecordCount { count: 3 };
        assert!(fail.evaluate(&actual).is_err());
    }

    #[test]
    fn test_field_exists_assertion() {
        let actual = vec![json!({"id": 1, "name": "A"}), json!({"id": 2, "name": "B"})];

        let pass = Assertion::FieldExists {
            field: "name".to_string(),
        };
        assert!(pass.evaluate(&actual).is_ok());

        let fail = Assertion::FieldExists {
            field: "missing".to_string(),
        };
        assert!(fail.evaluate(&actual).is_err());
    }

    #[test]
    fn test_field_not_exists_assertion() {
        let actual = vec![json!({"id": 1, "name": "A"}), json!({"id": 2, "name": "B"})];

        let pass = Assertion::FieldNotExists {
            field: "missing".to_string(),
        };
        assert!(pass.evaluate(&actual).is_ok());

        let fail = Assertion::FieldNotExists {
            field: "name".to_string(),
        };
        assert!(fail.evaluate(&actual).is_err());
    }

    #[test]
    fn test_field_value_assertion() {
        let actual = vec![json!({"status": "active"}), json!({"status": "active"})];

        let pass = Assertion::FieldValue {
            field: "status".to_string(),
            equals: json!("active"),
        };
        assert!(pass.evaluate(&actual).is_ok());

        let fail = Assertion::FieldValue {
            field: "status".to_string(),
            equals: json!("inactive"),
        };
        assert!(fail.evaluate(&actual).is_err());
    }

    #[test]
    fn test_generate_diff() {
        let expected = vec![json!({"id": 1}), json!({"id": 2})];
        let actual = vec![json!({"id": 1}), json!({"id": 99})];

        let diff = generate_diff(&expected, &actual);
        assert!(diff.contains("Record 1 mismatch:"));
        assert!(diff.contains("- Expected: {\"id\":2}"));
        assert!(diff.contains("+ Actual:   {\"id\":99}"));
    }
}
