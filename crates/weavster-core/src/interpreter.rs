//! Transform interpreter
//!
//! Applies transforms directly to JSON values without WASM compilation.

use std::borrow::Cow;
use std::sync::LazyLock;

use regex::Regex;
use serde_json::Value;

use crate::config::{
    DynamicJinjaContext, ErrorHandlingConfig, OnErrorBehavior, resolve_error_handling,
};
use crate::error::{Error, Result};
use crate::transforms::TransformConfig;

/// Regex matching dynamic Jinja expressions for runtime evaluation.
static DYNAMIC_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"\{\{\s*(now|uuid|timestamp)\(\)\s*\}\}").expect("valid regex"));

/// Apply a sequence of transforms to an input JSON value.
///
/// Errors are propagated immediately (stop-on-error behavior).
/// For configurable error handling, use [`apply_transforms_with_error_handling`].
///
/// The optional `dynamic_context` carries runtime Jinja expressions
/// (e.g., `{{ now() }}`, `{{ uuid() }}`) identified at config load time,
/// enabling future runtime evaluation of dynamic templates.
pub fn apply_transforms(
    input: &Value,
    transforms: &[TransformConfig],
    dynamic_context: Option<&DynamicJinjaContext>,
) -> Result<Value> {
    let mut current = input.clone();
    for transform in transforms {
        let evaluated = evaluate_dynamic_transform(transform, dynamic_context)?;
        current = apply_one(&current, &evaluated)?;
    }
    Ok(current)
}

/// Apply a sequence of transforms with error handling hierarchy.
///
/// Resolves error handling per-transform using the cascade:
/// global < flow < transform-level.
///
/// The optional `dynamic_context` carries runtime Jinja expressions
/// identified at config load time.
pub fn apply_transforms_with_error_handling(
    input: &Value,
    transforms: &[TransformConfig],
    global_error_handling: Option<&ErrorHandlingConfig>,
    flow_error_handling: Option<&ErrorHandlingConfig>,
    dynamic_context: Option<&DynamicJinjaContext>,
) -> Result<Value> {
    let mut current = input.clone();
    for transform in transforms {
        let evaluated = evaluate_dynamic_transform(transform, dynamic_context)?;
        let resolved_eh = resolve_error_handling(
            global_error_handling,
            flow_error_handling,
            evaluated.error_handling(),
        );
        match apply_one(&current, &evaluated) {
            Ok(value) => current = value,
            Err(e) => match resolved_eh.on_error {
                OnErrorBehavior::StopOnError => return Err(e),
                OnErrorBehavior::LogAndSkip => {
                    tracing::warn!(
                        error = %e,
                        "Transform error (log_and_skip): skipping transform"
                    );
                }
            },
        }
    }
    Ok(current)
}

fn apply_one(input: &Value, transform: &TransformConfig) -> Result<Value> {
    match transform {
        TransformConfig::Map { map, .. } => apply_map(input, map),
        TransformConfig::Drop { drop, .. } => apply_drop(input, drop),
        TransformConfig::AddFields { add_fields, .. } => apply_add_fields(input, add_fields),
        TransformConfig::Coalesce { coalesce, .. } => apply_coalesce(input, coalesce),
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

/// Evaluate dynamic Jinja expressions in a transform configuration.
///
/// Replaces runtime expressions (`{{ now() }}`, `{{ uuid() }}`, `{{ timestamp() }}`)
/// with their computed values. Called per-message to ensure fresh values.
///
/// Returns `Cow::Borrowed` when no dynamic evaluation is needed (avoiding a clone),
/// or `Cow::Owned` with the evaluated transform when dynamic expressions are present.
fn evaluate_dynamic_transform<'a>(
    transform: &'a TransformConfig,
    dynamic_context: Option<&DynamicJinjaContext>,
) -> Result<Cow<'a, TransformConfig>> {
    let needs_eval = dynamic_context
        .map(|ctx| !ctx.expressions.is_empty())
        .unwrap_or(false);

    if !needs_eval {
        return Ok(Cow::Borrowed(transform));
    }

    let yaml_value = serde_yaml::to_value(transform).map_err(|e| Error::TransformError {
        transform: "dynamic_eval".to_string(),
        message: format!("failed to serialize transform for dynamic evaluation: {e}"),
    })?;

    let replaced = replace_dynamic_in_value(yaml_value);

    let result = serde_yaml::from_value(replaced).map_err(|e| Error::TransformError {
        transform: "dynamic_eval".to_string(),
        message: format!("failed to deserialize transform after dynamic evaluation: {e}"),
    })?;
    Ok(Cow::Owned(result))
}

/// Recursively replace dynamic Jinja expressions in a serde_yaml::Value tree.
fn replace_dynamic_in_value(value: serde_yaml::Value) -> serde_yaml::Value {
    match value {
        serde_yaml::Value::String(ref s) if DYNAMIC_RE.is_match(s) => {
            let replaced = DYNAMIC_RE
                .replace_all(s, |caps: &regex::Captures| match &caps[1] {
                    "now" => chrono::Utc::now().to_rfc3339(),
                    "uuid" => uuid::Uuid::new_v4().to_string(),
                    "timestamp" => chrono::Utc::now().timestamp().to_string(),
                    _ => caps[0].to_string(),
                })
                .into_owned();
            serde_yaml::Value::String(replaced)
        }
        serde_yaml::Value::Mapping(map) => {
            let new_map = map
                .into_iter()
                .map(|(k, v)| (replace_dynamic_in_value(k), replace_dynamic_in_value(v)))
                .collect();
            serde_yaml::Value::Mapping(new_map)
        }
        serde_yaml::Value::Sequence(seq) => {
            serde_yaml::Value::Sequence(seq.into_iter().map(replace_dynamic_in_value).collect())
        }
        other => other,
    }
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
            error_handling: None,
        }];
        let result = apply_transforms(&input, &transforms, None).unwrap();
        assert_eq!(result["name"], "Alice");
        assert_eq!(result["first_name"], "Alice"); // original preserved
    }

    #[test]
    fn test_drop_removes_fields() {
        let input = json!({"a": 1, "b": 2, "c": 3});
        let transforms = vec![TransformConfig::Drop {
            drop: vec!["b".to_string()],
            error_handling: None,
        }];
        let result = apply_transforms(&input, &transforms, None).unwrap();
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
            error_handling: None,
        }];
        let result = apply_transforms(&input, &transforms, None).unwrap();
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
            error_handling: None,
        }];
        let result = apply_transforms(&input, &transforms, None).unwrap();
        assert_eq!(result["result"], "val");
    }

    #[test]
    fn test_coalesce_all_null_gives_null() {
        let input = json!({"a": null, "b": null});
        let transforms = vec![TransformConfig::Coalesce {
            coalesce: [("result".to_string(), vec!["a".to_string(), "b".to_string()])]
                .into_iter()
                .collect(),
            error_handling: None,
        }];
        let result = apply_transforms(&input, &transforms, None).unwrap();
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
                error_handling: None,
            },
            TransformConfig::Drop {
                drop: vec!["name".to_string(), "age".to_string()],
                error_handling: None,
            },
            TransformConfig::AddFields {
                add_fields: [("processed".to_string(), json!(true))]
                    .into_iter()
                    .collect(),
                error_handling: None,
            },
        ];
        let result = apply_transforms(&input, &transforms, None).unwrap();
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
            error_handling: None,
        }];
        let result = apply_transforms(&input, &transforms, None);
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
            error_handling: None,
        }];
        let result = apply_transforms(&input, &transforms, None).unwrap();
        assert_eq!(result["b"], Value::Null);
    }

    #[test]
    fn test_transform_level_error_handling_stop_on_error() {
        let input = json!({"a": 1});
        let transforms = vec![TransformConfig::Regex {
            regex: crate::transforms::RegexConfig {
                field: "a".to_string(),
                pattern: ".*".to_string(),
                captures: Default::default(),
                on_no_match: None,
            },
            error_handling: Some(ErrorHandlingConfig {
                on_error: OnErrorBehavior::StopOnError,
                log_level: "error".to_string(),
                retry: None,
            }),
        }];
        let result = apply_transforms_with_error_handling(&input, &transforms, None, None, None);
        assert!(result.is_err());
    }

    #[test]
    fn test_transform_level_error_handling_log_and_skip() {
        let input = json!({"a": 1});
        let transforms = vec![TransformConfig::Regex {
            regex: crate::transforms::RegexConfig {
                field: "a".to_string(),
                pattern: ".*".to_string(),
                captures: Default::default(),
                on_no_match: None,
            },
            error_handling: Some(ErrorHandlingConfig {
                on_error: OnErrorBehavior::LogAndSkip,
                log_level: "warn".to_string(),
                retry: None,
            }),
        }];
        // Should succeed because log_and_skip skips the failing transform
        let result = apply_transforms_with_error_handling(&input, &transforms, None, None, None);
        assert!(result.is_ok());
        assert_eq!(result.unwrap(), input);
    }

    #[test]
    fn test_transform_error_handling_overrides_flow() {
        let input = json!({"a": 1});
        let flow_eh = ErrorHandlingConfig {
            on_error: OnErrorBehavior::StopOnError,
            log_level: "error".to_string(),
            retry: None,
        };
        // Transform overrides flow-level stop_on_error with log_and_skip
        let transforms = vec![TransformConfig::Regex {
            regex: crate::transforms::RegexConfig {
                field: "a".to_string(),
                pattern: ".*".to_string(),
                captures: Default::default(),
                on_no_match: None,
            },
            error_handling: Some(ErrorHandlingConfig {
                on_error: OnErrorBehavior::LogAndSkip,
                log_level: "warn".to_string(),
                retry: None,
            }),
        }];
        let result =
            apply_transforms_with_error_handling(&input, &transforms, None, Some(&flow_eh), None);
        assert!(result.is_ok());
    }

    // =========================================================================
    // Dynamic Expression Evaluation Tests
    // =========================================================================

    #[test]
    fn test_dynamic_uuid_in_add_fields() {
        let input = json!({"name": "Alice"});
        let transforms = vec![TransformConfig::AddFields {
            add_fields: [(
                "id".to_string(),
                serde_json::Value::String("{{ uuid() }}".to_string()),
            )]
            .into_iter()
            .collect(),
            error_handling: None,
        }];
        let ctx = DynamicJinjaContext {
            expressions: vec!["{{ uuid() }}".to_string()],
        };
        let result = apply_transforms(&input, &transforms, Some(&ctx)).unwrap();
        let id = result["id"].as_str().unwrap();
        // UUID v4 format: 8-4-4-4-12 hex chars
        assert_eq!(id.len(), 36);
        assert!(id.contains('-'));
        assert_ne!(id, "{{ uuid() }}");
    }

    #[test]
    fn test_dynamic_now_in_add_fields() {
        let input = json!({"name": "Alice"});
        let transforms = vec![TransformConfig::AddFields {
            add_fields: [(
                "created_at".to_string(),
                serde_json::Value::String("{{ now() }}".to_string()),
            )]
            .into_iter()
            .collect(),
            error_handling: None,
        }];
        let ctx = DynamicJinjaContext {
            expressions: vec!["{{ now() }}".to_string()],
        };
        let result = apply_transforms(&input, &transforms, Some(&ctx)).unwrap();
        let created = result["created_at"].as_str().unwrap();
        // Should be an RFC3339 timestamp containing 'T'
        assert!(created.contains('T'));
        assert_ne!(created, "{{ now() }}");
    }

    #[test]
    fn test_dynamic_timestamp_in_add_fields() {
        let input = json!({"name": "Alice"});
        let transforms = vec![TransformConfig::AddFields {
            add_fields: [(
                "ts".to_string(),
                serde_json::Value::String("{{ timestamp() }}".to_string()),
            )]
            .into_iter()
            .collect(),
            error_handling: None,
        }];
        let ctx = DynamicJinjaContext {
            expressions: vec!["{{ timestamp() }}".to_string()],
        };
        let result = apply_transforms(&input, &transforms, Some(&ctx)).unwrap();
        let ts = result["ts"].as_str().unwrap();
        // Should be a numeric string (Unix timestamp)
        assert!(ts.parse::<i64>().is_ok());
        assert_ne!(ts, "{{ timestamp() }}");
    }

    #[test]
    fn test_no_dynamic_context_preserves_expressions() {
        let input = json!({"name": "Alice"});
        let transforms = vec![TransformConfig::AddFields {
            add_fields: [(
                "id".to_string(),
                serde_json::Value::String("{{ uuid() }}".to_string()),
            )]
            .into_iter()
            .collect(),
            error_handling: None,
        }];
        // No dynamic context - expressions should be preserved as-is
        let result = apply_transforms(&input, &transforms, None).unwrap();
        assert_eq!(result["id"], "{{ uuid() }}");
    }

    #[test]
    fn test_dynamic_expression_embedded_in_string() {
        let input = json!({"name": "Alice"});
        let transforms = vec![TransformConfig::AddFields {
            add_fields: [(
                "note".to_string(),
                serde_json::Value::String("created at {{ now() }} by system".to_string()),
            )]
            .into_iter()
            .collect(),
            error_handling: None,
        }];
        let ctx = DynamicJinjaContext {
            expressions: vec!["{{ now() }}".to_string()],
        };
        let result = apply_transforms(&input, &transforms, Some(&ctx)).unwrap();
        let note = result["note"].as_str().unwrap();
        assert!(note.starts_with("created at "));
        assert!(note.ends_with(" by system"));
        assert!(!note.contains("{{ now() }}"));
    }

    #[test]
    fn test_dynamic_per_message_generates_unique_uuids() {
        let input = json!({"name": "Alice"});
        let transforms = vec![TransformConfig::AddFields {
            add_fields: [(
                "id".to_string(),
                serde_json::Value::String("{{ uuid() }}".to_string()),
            )]
            .into_iter()
            .collect(),
            error_handling: None,
        }];
        let ctx = DynamicJinjaContext {
            expressions: vec!["{{ uuid() }}".to_string()],
        };
        // Each call should produce a different UUID
        let result1 = apply_transforms(&input, &transforms, Some(&ctx)).unwrap();
        let result2 = apply_transforms(&input, &transforms, Some(&ctx)).unwrap();
        assert_ne!(result1["id"], result2["id"]);
    }

    #[test]
    fn test_empty_dynamic_context_preserves_expressions() {
        let input = json!({"name": "Alice"});
        let transforms = vec![TransformConfig::AddFields {
            add_fields: [(
                "id".to_string(),
                serde_json::Value::String("{{ uuid() }}".to_string()),
            )]
            .into_iter()
            .collect(),
            error_handling: None,
        }];
        // Empty expressions list - should not evaluate
        let ctx = DynamicJinjaContext {
            expressions: vec![],
        };
        let result = apply_transforms(&input, &transforms, Some(&ctx)).unwrap();
        assert_eq!(result["id"], "{{ uuid() }}");
    }
}
