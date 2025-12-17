//! Intermediate Representation for transforms
//!
//! The IR is an abstract representation of a flow that's easier to
//! generate code from than raw YAML.

use std::collections::HashMap;

/// Intermediate representation of a complete flow
#[derive(Debug, Clone)]
pub struct FlowIR {
    /// Flow name (used for WASM module naming)
    pub name: String,

    /// Flow description
    pub description: Option<String>,

    /// Input connector reference
    pub input: String,

    /// Ordered list of transform operations
    pub transforms: Vec<TransformIR>,

    /// Output configurations
    pub outputs: Vec<OutputIR>,

    /// Embedded artifacts (translation tables, etc.)
    pub artifacts: Vec<ArtifactIR>,
}

/// IR for a single transform operation
#[derive(Debug, Clone)]
pub enum TransformIR {
    /// Direct field mapping: target <- source
    Map(Vec<FieldMapping>),

    /// Regex extraction
    Regex(RegexTransform),

    /// Jinja template rendering
    Template(Vec<TemplateField>),

    /// Lookup table reference
    Lookup(LookupTransform),

    /// Filter (conditional pass-through)
    Filter(FilterTransform),

    /// Drop fields
    Drop(Vec<String>),

    /// Coalesce (first non-null)
    Coalesce(Vec<CoalesceField>),
}

/// A single field mapping
#[derive(Debug, Clone)]
pub struct FieldMapping {
    /// Target field name in output
    pub target: String,

    /// Source field path (e.g., "source.customer.id")
    pub source: String,

    /// Optional default value if source is null
    pub default: Option<serde_json::Value>,
}

/// Regex-based transform
#[derive(Debug, Clone)]
pub struct RegexTransform {
    /// Source field to match against
    pub source_field: String,

    /// Regex pattern
    pub pattern: String,

    /// Named capture group mappings: output_field -> capture_group
    pub captures: HashMap<String, CaptureMapping>,

    /// What to do if no match: null, skip, or error
    pub on_no_match: NoMatchBehavior,
}

/// How to map a regex capture
#[derive(Debug, Clone)]
pub struct CaptureMapping {
    /// Capture group (by index or name)
    pub group: CaptureGroup,

    /// Optional transformation to apply
    pub transform: Option<CaptureTransform>,
}

/// Capture group identifier
#[derive(Debug, Clone)]
pub enum CaptureGroup {
    /// Numbered group (0 is entire match)
    Index(usize),
    /// Named group
    Named(String),
}

/// Post-capture transformation
#[derive(Debug, Clone)]
pub enum CaptureTransform {
    /// Convert to uppercase
    Uppercase,
    /// Convert to lowercase
    Lowercase,
    /// Trim whitespace
    Trim,
    /// Parse as integer
    ParseInt,
    /// Parse as float
    ParseFloat,
}

/// Behavior when regex doesn't match
#[derive(Debug, Clone, Default)]
pub enum NoMatchBehavior {
    /// Set output fields to null
    #[default]
    Null,
    /// Skip this transform (pass through unchanged)
    Skip,
    /// Fail the transform
    Error,
}

/// Template-based field generation
#[derive(Debug, Clone)]
pub struct TemplateField {
    /// Output field name
    pub target: String,

    /// Jinja template string
    pub template: String,
}

/// Lookup table transform
#[derive(Debug, Clone)]
pub struct LookupTransform {
    /// Source field containing the lookup key
    pub key_field: String,

    /// Name of the artifact containing the lookup table
    pub table: String,

    /// Column in the table to use as key (if CSV)
    pub key_column: Option<String>,

    /// Column in the table to use as value (if CSV)
    pub value_column: Option<String>,

    /// Output field name
    pub output_field: String,

    /// Default value if key not found
    pub default: Option<serde_json::Value>,
}

/// Filter transform
#[derive(Debug, Clone)]
pub struct FilterTransform {
    /// Condition expression
    pub condition: FilterCondition,
}

/// Filter condition
#[derive(Debug, Clone)]
pub enum FilterCondition {
    /// Simple field comparison
    Compare {
        /// Field to compare
        field: String,
        /// Comparison operator
        op: CompareOp,
        /// Value to compare against
        value: serde_json::Value,
    },

    /// Field is not null
    NotNull(String),

    /// Field is null
    IsNull(String),

    /// Regex match
    Matches {
        /// Field to match
        field: String,
        /// Regex pattern
        pattern: String,
    },

    /// Logical AND
    And(Vec<FilterCondition>),

    /// Logical OR
    Or(Vec<FilterCondition>),

    /// Logical NOT
    Not(Box<FilterCondition>),

    /// Raw expression (for complex cases)
    Expression(String),
}

/// Comparison operator
#[derive(Debug, Clone, Copy)]
pub enum CompareOp {
    /// Equal
    Eq,
    /// Not equal
    Ne,
    /// Greater than
    Gt,
    /// Greater than or equal
    Ge,
    /// Less than
    Lt,
    /// Less than or equal
    Le,
    /// Contains (for strings/arrays)
    Contains,
    /// Starts with (for strings)
    StartsWith,
    /// Ends with (for strings)
    EndsWith,
}

/// Coalesce field definition
#[derive(Debug, Clone)]
pub struct CoalesceField {
    /// Output field name
    pub target: String,

    /// Source fields to try in order
    pub sources: Vec<String>,
}

/// Output configuration
#[derive(Debug, Clone)]
pub struct OutputIR {
    /// Connector reference
    pub connector: String,

    /// Optional filter condition
    pub condition: Option<FilterCondition>,
}

/// Embedded artifact
#[derive(Debug, Clone)]
pub struct ArtifactIR {
    /// Artifact name (referenced by transforms)
    pub name: String,

    /// Artifact type
    pub kind: ArtifactKind,

    /// Artifact data
    pub data: ArtifactData,
}

/// Type of artifact
#[derive(Debug, Clone)]
pub enum ArtifactKind {
    /// CSV lookup table
    LookupTable,
    /// JSON configuration
    JsonConfig,
    /// Regex pattern collection
    RegexPatterns,
}

/// Artifact data storage
#[derive(Debug, Clone)]
pub enum ArtifactData {
    /// Key-value pairs (for lookup tables)
    KeyValue(HashMap<String, String>),

    /// Raw JSON
    Json(serde_json::Value),

    /// Raw string content
    Raw(String),
}

impl FlowIR {
    /// Create a new empty FlowIR
    pub fn new(name: impl Into<String>) -> Self {
        Self {
            name: name.into(),
            description: None,
            input: String::new(),
            transforms: Vec::new(),
            outputs: Vec::new(),
            artifacts: Vec::new(),
        }
    }

    /// Get a hash of the IR for cache invalidation
    pub fn content_hash(&self) -> String {
        use sha2::{Digest, Sha256};

        let mut hasher = Sha256::new();

        // Hash the flow structure
        hasher.update(self.name.as_bytes());
        hasher.update(self.input.as_bytes());

        // Hash transforms (simplified - in practice would be more thorough)
        hasher.update(format!("{:?}", self.transforms).as_bytes());

        // Hash artifacts
        for artifact in &self.artifacts {
            hasher.update(artifact.name.as_bytes());
            hasher.update(format!("{:?}", artifact.data).as_bytes());
        }

        hex::encode(hasher.finalize())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_flow_ir_creation() {
        let ir = FlowIR::new("test_flow");
        assert_eq!(ir.name, "test_flow");
        assert!(ir.transforms.is_empty());
    }

    #[test]
    fn test_flow_ir_new_with_string() {
        let ir = FlowIR::new(String::from("my_flow"));
        assert_eq!(ir.name, "my_flow");
        assert!(ir.description.is_none());
        assert!(ir.input.is_empty());
        assert!(ir.transforms.is_empty());
        assert!(ir.outputs.is_empty());
        assert!(ir.artifacts.is_empty());
    }

    #[test]
    fn test_content_hash_changes() {
        let mut ir1 = FlowIR::new("test");
        ir1.input = "kafka.topic1".to_string();

        let mut ir2 = FlowIR::new("test");
        ir2.input = "kafka.topic2".to_string();

        assert_ne!(ir1.content_hash(), ir2.content_hash());
    }

    #[test]
    fn test_content_hash_same_for_identical() {
        let mut ir1 = FlowIR::new("test");
        ir1.input = "kafka.topic".to_string();

        let mut ir2 = FlowIR::new("test");
        ir2.input = "kafka.topic".to_string();

        assert_eq!(ir1.content_hash(), ir2.content_hash());
    }

    #[test]
    fn test_content_hash_changes_with_transforms() {
        let mut ir1 = FlowIR::new("test");
        ir1.input = "kafka.topic".to_string();
        ir1.transforms
            .push(TransformIR::Drop(vec!["field1".to_string()]));

        let mut ir2 = FlowIR::new("test");
        ir2.input = "kafka.topic".to_string();
        ir2.transforms
            .push(TransformIR::Drop(vec!["field2".to_string()]));

        assert_ne!(ir1.content_hash(), ir2.content_hash());
    }

    #[test]
    fn test_content_hash_changes_with_artifacts() {
        let mut ir1 = FlowIR::new("test");
        ir1.artifacts.push(ArtifactIR {
            name: "lookup".to_string(),
            kind: ArtifactKind::LookupTable,
            data: ArtifactData::Raw("data1".to_string()),
        });

        let mut ir2 = FlowIR::new("test");
        ir2.artifacts.push(ArtifactIR {
            name: "lookup".to_string(),
            kind: ArtifactKind::LookupTable,
            data: ArtifactData::Raw("data2".to_string()),
        });

        assert_ne!(ir1.content_hash(), ir2.content_hash());
    }

    #[test]
    fn test_field_mapping() {
        let mapping = FieldMapping {
            target: "output_field".to_string(),
            source: "input_field".to_string(),
            default: Some(serde_json::json!("default_value")),
        };

        assert_eq!(mapping.target, "output_field");
        assert_eq!(mapping.source, "input_field");
        assert_eq!(mapping.default, Some(serde_json::json!("default_value")));
    }

    #[test]
    fn test_regex_transform() {
        let mut captures = HashMap::new();
        captures.insert(
            "date".to_string(),
            CaptureMapping {
                group: CaptureGroup::Index(1),
                transform: None,
            },
        );

        let regex = RegexTransform {
            source_field: "message".to_string(),
            pattern: r"(\d{4}-\d{2}-\d{2})".to_string(),
            captures,
            on_no_match: NoMatchBehavior::Null,
        };

        assert_eq!(regex.source_field, "message");
        assert!(regex.captures.contains_key("date"));
    }

    #[test]
    fn test_capture_group_index() {
        let group = CaptureGroup::Index(3);
        match group {
            CaptureGroup::Index(i) => assert_eq!(i, 3),
            _ => panic!("Expected Index"),
        }
    }

    #[test]
    fn test_capture_group_named() {
        let group = CaptureGroup::Named("timestamp".to_string());
        match group {
            CaptureGroup::Named(n) => assert_eq!(n, "timestamp"),
            _ => panic!("Expected Named"),
        }
    }

    #[test]
    fn test_capture_transform_variants() {
        let _ = CaptureTransform::Uppercase;
        let _ = CaptureTransform::Lowercase;
        let _ = CaptureTransform::Trim;
        let _ = CaptureTransform::ParseInt;
        let _ = CaptureTransform::ParseFloat;
    }

    #[test]
    fn test_no_match_behavior_default() {
        let behavior = NoMatchBehavior::default();
        assert!(matches!(behavior, NoMatchBehavior::Null));
    }

    #[test]
    fn test_template_field() {
        let field = TemplateField {
            target: "greeting".to_string(),
            template: "Hello, {{ name }}!".to_string(),
        };

        assert_eq!(field.target, "greeting");
        assert!(field.template.contains("{{ name }}"));
    }

    #[test]
    fn test_lookup_transform() {
        let lookup = LookupTransform {
            key_field: "country_code".to_string(),
            table: "countries".to_string(),
            key_column: Some("code".to_string()),
            value_column: Some("name".to_string()),
            output_field: "country_name".to_string(),
            default: Some(serde_json::json!("Unknown")),
        };

        assert_eq!(lookup.key_field, "country_code");
        assert_eq!(lookup.table, "countries");
        assert_eq!(lookup.key_column, Some("code".to_string()));
        assert_eq!(lookup.default, Some(serde_json::json!("Unknown")));
    }

    #[test]
    fn test_filter_condition_compare() {
        let condition = FilterCondition::Compare {
            field: "age".to_string(),
            op: CompareOp::Gt,
            value: serde_json::json!(18),
        };

        match condition {
            FilterCondition::Compare { field, op, value } => {
                assert_eq!(field, "age");
                assert!(matches!(op, CompareOp::Gt));
                assert_eq!(value, serde_json::json!(18));
            }
            _ => panic!("Expected Compare"),
        }
    }

    #[test]
    fn test_filter_condition_not_null() {
        let condition = FilterCondition::NotNull("email".to_string());
        match condition {
            FilterCondition::NotNull(field) => assert_eq!(field, "email"),
            _ => panic!("Expected NotNull"),
        }
    }

    #[test]
    fn test_filter_condition_is_null() {
        let condition = FilterCondition::IsNull("optional_field".to_string());
        match condition {
            FilterCondition::IsNull(field) => assert_eq!(field, "optional_field"),
            _ => panic!("Expected IsNull"),
        }
    }

    #[test]
    fn test_filter_condition_matches() {
        let condition = FilterCondition::Matches {
            field: "email".to_string(),
            pattern: r".*@.*\.com".to_string(),
        };

        match condition {
            FilterCondition::Matches { field, pattern } => {
                assert_eq!(field, "email");
                assert!(pattern.contains("@"));
            }
            _ => panic!("Expected Matches"),
        }
    }

    #[test]
    fn test_filter_condition_and() {
        let condition = FilterCondition::And(vec![
            FilterCondition::NotNull("field1".to_string()),
            FilterCondition::NotNull("field2".to_string()),
        ]);

        match condition {
            FilterCondition::And(conditions) => assert_eq!(conditions.len(), 2),
            _ => panic!("Expected And"),
        }
    }

    #[test]
    fn test_filter_condition_or() {
        let condition = FilterCondition::Or(vec![
            FilterCondition::IsNull("a".to_string()),
            FilterCondition::IsNull("b".to_string()),
        ]);

        match condition {
            FilterCondition::Or(conditions) => assert_eq!(conditions.len(), 2),
            _ => panic!("Expected Or"),
        }
    }

    #[test]
    fn test_filter_condition_not() {
        let condition =
            FilterCondition::Not(Box::new(FilterCondition::IsNull("field".to_string())));

        match condition {
            FilterCondition::Not(inner) => {
                assert!(matches!(*inner, FilterCondition::IsNull(_)));
            }
            _ => panic!("Expected Not"),
        }
    }

    #[test]
    fn test_filter_condition_expression() {
        let condition = FilterCondition::Expression("x > 10 && y < 20".to_string());
        match condition {
            FilterCondition::Expression(expr) => assert!(expr.contains("&&")),
            _ => panic!("Expected Expression"),
        }
    }

    #[test]
    fn test_compare_op_variants() {
        let _ = CompareOp::Eq;
        let _ = CompareOp::Ne;
        let _ = CompareOp::Gt;
        let _ = CompareOp::Ge;
        let _ = CompareOp::Lt;
        let _ = CompareOp::Le;
        let _ = CompareOp::Contains;
        let _ = CompareOp::StartsWith;
        let _ = CompareOp::EndsWith;
    }

    #[test]
    fn test_coalesce_field() {
        let field = CoalesceField {
            target: "email".to_string(),
            sources: vec!["primary_email".to_string(), "secondary_email".to_string()],
        };

        assert_eq!(field.target, "email");
        assert_eq!(field.sources.len(), 2);
    }

    #[test]
    fn test_output_ir() {
        let output = OutputIR {
            connector: "kafka.output".to_string(),
            condition: Some(FilterCondition::Expression(
                "status == 'active'".to_string(),
            )),
        };

        assert_eq!(output.connector, "kafka.output");
        assert!(output.condition.is_some());
    }

    #[test]
    fn test_artifact_ir() {
        let mut map = HashMap::new();
        map.insert("key1".to_string(), "value1".to_string());

        let artifact = ArtifactIR {
            name: "lookup_table".to_string(),
            kind: ArtifactKind::LookupTable,
            data: ArtifactData::KeyValue(map),
        };

        assert_eq!(artifact.name, "lookup_table");
        assert!(matches!(artifact.kind, ArtifactKind::LookupTable));
    }

    #[test]
    fn test_artifact_kind_variants() {
        let _ = ArtifactKind::LookupTable;
        let _ = ArtifactKind::JsonConfig;
        let _ = ArtifactKind::RegexPatterns;
    }

    #[test]
    fn test_artifact_data_key_value() {
        let mut map = HashMap::new();
        map.insert("A".to_string(), "Alpha".to_string());

        let data = ArtifactData::KeyValue(map);
        match data {
            ArtifactData::KeyValue(m) => {
                assert_eq!(m.get("A"), Some(&"Alpha".to_string()));
            }
            _ => panic!("Expected KeyValue"),
        }
    }

    #[test]
    fn test_artifact_data_json() {
        let json = serde_json::json!({"key": "value"});
        let data = ArtifactData::Json(json.clone());

        match data {
            ArtifactData::Json(j) => assert_eq!(j, json),
            _ => panic!("Expected Json"),
        }
    }

    #[test]
    fn test_artifact_data_raw() {
        let data = ArtifactData::Raw("raw content".to_string());
        match data {
            ArtifactData::Raw(s) => assert_eq!(s, "raw content"),
            _ => panic!("Expected Raw"),
        }
    }

    #[test]
    fn test_transform_ir_map() {
        let transform = TransformIR::Map(vec![FieldMapping {
            target: "out".to_string(),
            source: "in".to_string(),
            default: None,
        }]);

        assert!(matches!(transform, TransformIR::Map(_)));
    }

    #[test]
    fn test_transform_ir_drop() {
        let transform = TransformIR::Drop(vec!["field1".to_string(), "field2".to_string()]);

        match transform {
            TransformIR::Drop(fields) => assert_eq!(fields.len(), 2),
            _ => panic!("Expected Drop"),
        }
    }

    #[test]
    fn test_transform_ir_coalesce() {
        let transform = TransformIR::Coalesce(vec![CoalesceField {
            target: "result".to_string(),
            sources: vec!["a".to_string(), "b".to_string()],
        }]);

        assert!(matches!(transform, TransformIR::Coalesce(_)));
    }

    #[test]
    fn test_flow_ir_clone() {
        let mut ir = FlowIR::new("test");
        ir.input = "kafka.input".to_string();
        ir.transforms.push(TransformIR::Drop(vec!["x".to_string()]));

        let cloned = ir.clone();
        assert_eq!(ir.name, cloned.name);
        assert_eq!(ir.input, cloned.input);
        assert_eq!(ir.transforms.len(), cloned.transforms.len());
    }
}
