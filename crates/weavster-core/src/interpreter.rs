//! Transform interpreter
//!
//! Applies transforms directly to JSON values without WASM compilation.

use serde_json::Value;

use crate::error::{Error, Result};
use crate::transforms::TransformConfig;

/// Apply a sequence of transforms to an input JSON value.
pub fn apply_transforms(input: &Value, transforms: &[TransformConfig]) -> Result<Value> {
    let mut current = input.clone();
    for transform in transforms {
        current = apply_one(&current, transform)?;
    }
    Ok(current)
}

fn apply_one(input: &Value, transform: &TransformConfig) -> Result<Value> {
    match transform {
        TransformConfig::Map { map } => apply_map(input, map),
        TransformConfig::Drop { drop } => apply_drop(input, drop),
        TransformConfig::AddFields { add_fields } => apply_add_fields(input, add_fields),
        TransformConfig::Coalesce { coalesce } => apply_coalesce(input, coalesce),
        TransformConfig::Regex { .. } => Err(Error::TransformError {
            transform: "regex".to_string(),
            message: "not yet supported by the interpreter".to_string(),
        }),
        TransformConfig::Template { .. } => Err(Error::TransformError {
            transform: "template".to_string(),
            message: "not yet supported by the interpreter".to_string(),
        }),
        TransformConfig::Lookup { .. } => Err(Error::TransformError {
            transform: "lookup".to_string(),
            message: "not yet supported by the interpreter".to_string(),
        }),
        TransformConfig::Filter { .. } => Err(Error::TransformError {
            transform: "filter".to_string(),
            message: "not yet supported by the interpreter".to_string(),
        }),
    }
}

fn apply_map(input: &Value, mappings: &std::collections::HashMap<String, String>) -> Result<Value> {
    let obj = input.as_object().ok_or_else(|| Error::TransformError {
        transform: "map".to_string(),
        message: "input is not a JSON object".to_string(),
    })?;

    let mut output = obj.clone();
    for (output_field, input_field) in mappings {
        let value = obj
            .get(input_field.as_str())
            .cloned()
            .unwrap_or(Value::Null);
        output.insert(output_field.clone(), value);
    }
    Ok(Value::Object(output))
}

fn apply_drop(input: &Value, fields: &[String]) -> Result<Value> {
    let mut obj = input
        .as_object()
        .ok_or_else(|| Error::TransformError {
            transform: "drop".to_string(),
            message: "input is not a JSON object".to_string(),
        })?
        .clone();

    for field in fields {
        obj.remove(field);
    }
    Ok(Value::Object(obj))
}

fn apply_add_fields(
    input: &Value,
    fields: &std::collections::HashMap<String, Value>,
) -> Result<Value> {
    let mut obj = input
        .as_object()
        .ok_or_else(|| Error::TransformError {
            transform: "add_fields".to_string(),
            message: "input is not a JSON object".to_string(),
        })?
        .clone();

    for (key, value) in fields {
        obj.insert(key.clone(), value.clone());
    }
    Ok(Value::Object(obj))
}

fn apply_coalesce(
    input: &Value,
    mappings: &std::collections::HashMap<String, Vec<String>>,
) -> Result<Value> {
    let obj = input.as_object().ok_or_else(|| Error::TransformError {
        transform: "coalesce".to_string(),
        message: "input is not a JSON object".to_string(),
    })?;

    let mut output = obj.clone();
    for (output_field, source_fields) in mappings {
        let value = source_fields
            .iter()
            .find_map(|f| obj.get(f).filter(|v| !v.is_null()).cloned())
            .unwrap_or(Value::Null);
        output.insert(output_field.clone(), value);
    }
    Ok(Value::Object(output))
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn test_map_renames_fields() {
        let input = json!({"first_name": "Alice", "age": 30});
        let transforms = vec![TransformConfig::Map {
            map: [("name".to_string(), "first_name".to_string())]
                .into_iter()
                .collect(),
        }];
        let result = apply_transforms(&input, &transforms).unwrap();
        assert_eq!(result["name"], "Alice");
        assert_eq!(result["first_name"], "Alice"); // original preserved
    }

    #[test]
    fn test_drop_removes_fields() {
        let input = json!({"a": 1, "b": 2, "c": 3});
        let transforms = vec![TransformConfig::Drop {
            drop: vec!["b".to_string()],
        }];
        let result = apply_transforms(&input, &transforms).unwrap();
        assert_eq!(result["a"], 1);
        assert!(result.get("b").is_none());
        assert_eq!(result["c"], 3);
    }

    #[test]
    fn test_add_fields_inserts_values() {
        let input = json!({"a": 1});
        let transforms = vec![TransformConfig::AddFields {
            add_fields: [("processed".to_string(), json!(true))]
                .into_iter()
                .collect(),
        }];
        let result = apply_transforms(&input, &transforms).unwrap();
        assert_eq!(result["a"], 1);
        assert_eq!(result["processed"], true);
    }

    #[test]
    fn test_coalesce_picks_first_non_null() {
        let input = json!({"a": null, "b": "val", "c": "other"});
        let transforms = vec![TransformConfig::Coalesce {
            coalesce: [(
                "result".to_string(),
                vec!["a".to_string(), "b".to_string(), "c".to_string()],
            )]
            .into_iter()
            .collect(),
        }];
        let result = apply_transforms(&input, &transforms).unwrap();
        assert_eq!(result["result"], "val");
    }

    #[test]
    fn test_coalesce_all_null_gives_null() {
        let input = json!({"a": null, "b": null});
        let transforms = vec![TransformConfig::Coalesce {
            coalesce: [("result".to_string(), vec!["a".to_string(), "b".to_string()])]
                .into_iter()
                .collect(),
        }];
        let result = apply_transforms(&input, &transforms).unwrap();
        assert_eq!(result["result"], Value::Null);
    }

    #[test]
    fn test_combined_transforms() {
        let input = json!({"name": "Alice", "email": "alice@example.com", "age": 30});
        let transforms = vec![
            TransformConfig::Map {
                map: [("full_name".to_string(), "name".to_string())]
                    .into_iter()
                    .collect(),
            },
            TransformConfig::Drop {
                drop: vec!["name".to_string(), "age".to_string()],
            },
            TransformConfig::AddFields {
                add_fields: [("processed".to_string(), json!(true))]
                    .into_iter()
                    .collect(),
            },
        ];
        let result = apply_transforms(&input, &transforms).unwrap();
        assert_eq!(result["full_name"], "Alice");
        assert_eq!(result["email"], "alice@example.com");
        assert_eq!(result["processed"], true);
        assert!(result.get("name").is_none());
        assert!(result.get("age").is_none());
    }

    #[test]
    fn test_unsupported_transform_errors() {
        let input = json!({"a": 1});
        let transforms = vec![TransformConfig::Regex {
            regex: crate::transforms::RegexConfig {
                field: "a".to_string(),
                pattern: ".*".to_string(),
                captures: Default::default(),
                on_no_match: None,
            },
        }];
        let result = apply_transforms(&input, &transforms);
        assert!(result.is_err());
        let err = result.unwrap_err().to_string();
        assert!(err.contains("not yet supported"));
    }

    #[test]
    fn test_map_missing_source_gives_null() {
        let input = json!({"a": 1});
        let transforms = vec![TransformConfig::Map {
            map: [("b".to_string(), "nonexistent".to_string())]
                .into_iter()
                .collect(),
        }];
        let result = apply_transforms(&input, &transforms).unwrap();
        assert_eq!(result["b"], Value::Null);
    }
}
