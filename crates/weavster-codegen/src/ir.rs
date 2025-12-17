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
    fn test_content_hash_changes() {
        let mut ir1 = FlowIR::new("test");
        ir1.input = "kafka.topic1".to_string();

        let mut ir2 = FlowIR::new("test");
        ir2.input = "kafka.topic2".to_string();

        assert_ne!(ir1.content_hash(), ir2.content_hash());
    }
}
