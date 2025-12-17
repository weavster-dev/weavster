//! YAML to IR parser
//!
//! Parses flow YAML configuration into the intermediate representation.

use std::collections::HashMap;
use std::path::Path;

use crate::error::{Error, Result};
use crate::ir::*;

/// Parser for flow YAML files
pub struct Parser {
    /// Base path for resolving artifact references
    #[allow(dead_code)]
    base_path: std::path::PathBuf,
}

impl Parser {
    /// Create a new parser with the given base path
    pub fn new(base_path: impl AsRef<Path>) -> Self {
        Self {
            base_path: base_path.as_ref().to_path_buf(),
        }
    }

    /// Parse a flow YAML file into IR
    pub fn parse_file(&self, path: impl AsRef<Path>) -> Result<FlowIR> {
        let content = std::fs::read_to_string(path.as_ref())?;
        self.parse_yaml(&content)
    }

    /// Parse YAML string into IR
    pub fn parse_yaml(&self, yaml: &str) -> Result<FlowIR> {
        let raw: RawFlow = serde_yaml::from_str(yaml)?;
        self.convert_to_ir(raw)
    }

    fn convert_to_ir(&self, raw: RawFlow) -> Result<FlowIR> {
        let mut ir = FlowIR::new(&raw.name);
        ir.description = raw.description;
        ir.input = raw.input;

        // Convert transforms
        for raw_transform in raw.transforms {
            let transform_ir = self.convert_transform(&raw.name, raw_transform)?;
            ir.transforms.push(transform_ir);
        }

        // Convert outputs
        for raw_output in raw.outputs {
            ir.outputs.push(self.convert_output(raw_output)?);
        }

        Ok(ir)
    }

    fn convert_transform(&self, _flow_name: &str, raw: RawTransform) -> Result<TransformIR> {
        match raw {
            RawTransform::Map { map: mappings } => {
                let field_mappings = mappings
                    .into_iter()
                    .map(|(target, source)| FieldMapping {
                        target,
                        source,
                        default: None,
                    })
                    .collect();
                Ok(TransformIR::Map(field_mappings))
            }

            RawTransform::Regex { regex } => {
                // Validate regex pattern
                regex::Regex::new(&regex.pattern).map_err(|e| Error::InvalidRegex {
                    pattern: regex.pattern.clone(),
                    message: e.to_string(),
                })?;

                let capture_mappings = regex
                    .captures
                    .into_iter()
                    .map(|(output, group)| {
                        let capture_group = if let Ok(idx) = group.parse::<usize>() {
                            CaptureGroup::Index(idx)
                        } else {
                            CaptureGroup::Named(group)
                        };
                        (
                            output,
                            CaptureMapping {
                                group: capture_group,
                                transform: None,
                            },
                        )
                    })
                    .collect();

                Ok(TransformIR::Regex(RegexTransform {
                    source_field: regex.field,
                    pattern: regex.pattern,
                    captures: capture_mappings,
                    on_no_match: regex.on_no_match.unwrap_or_default().into(),
                }))
            }

            RawTransform::Template {
                template: templates,
            } => {
                let fields = templates
                    .into_iter()
                    .map(|(target, template)| TemplateField { target, template })
                    .collect();
                Ok(TransformIR::Template(fields))
            }

            RawTransform::Lookup { lookup } => Ok(TransformIR::Lookup(LookupTransform {
                key_field: lookup.field,
                table: lookup.table,
                key_column: None,
                value_column: None,
                output_field: lookup.output,
                default: lookup.default,
            })),

            RawTransform::Filter { filter } => Ok(TransformIR::Filter(FilterTransform {
                condition: FilterCondition::Expression(filter.when),
            })),

            RawTransform::Drop { drop: fields } => Ok(TransformIR::Drop(fields)),

            RawTransform::Coalesce { coalesce: mappings } => {
                let fields = mappings
                    .into_iter()
                    .map(|(target, sources)| CoalesceField { target, sources })
                    .collect();
                Ok(TransformIR::Coalesce(fields))
            }
        }
    }

    fn convert_output(&self, raw: RawOutput) -> Result<OutputIR> {
        match raw {
            RawOutput::Simple(connector) => Ok(OutputIR {
                connector,
                condition: None,
            }),
            RawOutput::Conditional { connector, when } => Ok(OutputIR {
                connector,
                condition: Some(FilterCondition::Expression(when)),
            }),
        }
    }
}

// ============================================================================
// Raw YAML structures (for serde deserialization)
// ============================================================================

use serde::Deserialize;

#[derive(Debug, Deserialize)]
struct RawFlow {
    name: String,
    #[serde(default)]
    description: Option<String>,
    input: String,
    #[serde(default)]
    transforms: Vec<RawTransform>,
    #[serde(default)]
    outputs: Vec<RawOutput>,
}

/// Helper struct for regex transform YAML parsing
#[derive(Debug, Deserialize)]
struct RegexTransformYaml {
    field: String,
    pattern: String,
    captures: HashMap<String, String>,
    #[serde(default)]
    on_no_match: Option<String>,
}

/// Helper struct for lookup transform YAML parsing
#[derive(Debug, Deserialize)]
struct LookupTransformYaml {
    field: String,
    table: String,
    output: String,
    #[serde(default)]
    default: Option<serde_json::Value>,
}

/// Helper struct for filter transform YAML parsing
#[derive(Debug, Deserialize)]
struct FilterTransformYaml {
    when: String,
}

#[derive(Debug, Deserialize)]
#[serde(untagged)]
enum RawTransform {
    Map {
        map: HashMap<String, String>,
    },
    Regex {
        regex: RegexTransformYaml,
    },
    Template {
        template: HashMap<String, String>,
    },
    Lookup {
        lookup: LookupTransformYaml,
    },
    Filter {
        filter: FilterTransformYaml,
    },
    Drop {
        drop: Vec<String>,
    },
    Coalesce {
        coalesce: HashMap<String, Vec<String>>,
    },
}

#[derive(Debug, Deserialize)]
#[serde(untagged)]
enum RawOutput {
    Simple(String),
    Conditional { connector: String, when: String },
}

impl From<String> for NoMatchBehavior {
    fn from(s: String) -> Self {
        match s.to_lowercase().as_str() {
            "skip" => NoMatchBehavior::Skip,
            "error" => NoMatchBehavior::Error,
            _ => NoMatchBehavior::Null,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parser_new() {
        let parser = Parser::new("/some/path");
        assert_eq!(parser.base_path.to_str().unwrap(), "/some/path");
    }

    #[test]
    fn test_parse_simple_flow() {
        let yaml = r#"
name: test_flow
input: kafka.orders
transforms:
  - map:
      customer_id: cust_id
      order_total: total
outputs:
  - postgres.orders
"#;
        let parser = Parser::new(".");
        let ir = parser.parse_yaml(yaml).unwrap();

        assert_eq!(ir.name, "test_flow");
        assert_eq!(ir.input, "kafka.orders");
        assert_eq!(ir.transforms.len(), 1);
        assert_eq!(ir.outputs.len(), 1);
    }

    #[test]
    fn test_parse_flow_with_description() {
        let yaml = r#"
name: described_flow
description: "This flow processes orders"
input: kafka.orders
outputs:
  - postgres.orders
"#;
        let parser = Parser::new(".");
        let ir = parser.parse_yaml(yaml).unwrap();

        assert_eq!(ir.name, "described_flow");
        assert_eq!(
            ir.description,
            Some("This flow processes orders".to_string())
        );
    }

    #[test]
    fn test_parse_template_transform() {
        let yaml = r#"
name: template_flow
input: kafka.orders
transforms:
  - template:
      full_name: "{{ first_name }} {{ last_name }}"
outputs:
  - postgres.orders
"#;
        let parser = Parser::new(".");
        let ir = parser.parse_yaml(yaml).unwrap();

        match &ir.transforms[0] {
            TransformIR::Template(fields) => {
                assert_eq!(fields.len(), 1);
                assert_eq!(fields[0].target, "full_name");
                assert!(fields[0].template.contains("first_name"));
            }
            _ => panic!("Expected template transform"),
        }
    }

    #[test]
    fn test_parse_regex_transform() {
        let yaml = r#"
name: regex_flow
input: kafka.logs
transforms:
  - regex:
      field: message
      pattern: '(\d{4}-\d{2}-\d{2}) (.+)'
      captures:
        date: "1"
        content: "2"
outputs:
  - postgres.parsed_logs
"#;
        let parser = Parser::new(".");
        let ir = parser.parse_yaml(yaml).unwrap();

        match &ir.transforms[0] {
            TransformIR::Regex(regex) => {
                assert_eq!(regex.source_field, "message");
                assert!(regex.pattern.contains(r"\d{4}"));
                assert_eq!(regex.captures.len(), 2);
                assert!(regex.captures.contains_key("date"));
                assert!(regex.captures.contains_key("content"));
            }
            _ => panic!("Expected regex transform"),
        }
    }

    #[test]
    fn test_parse_regex_transform_with_named_groups() {
        let yaml = r#"
name: named_regex_flow
input: kafka.logs
transforms:
  - regex:
      field: message
      pattern: '(?P<timestamp>\d+) (?P<level>\w+)'
      captures:
        ts: timestamp
        log_level: level
outputs:
  - postgres.logs
"#;
        let parser = Parser::new(".");
        let ir = parser.parse_yaml(yaml).unwrap();

        match &ir.transforms[0] {
            TransformIR::Regex(regex) => {
                // Named captures should be parsed as Named
                let ts_capture = regex.captures.get("ts").unwrap();
                match &ts_capture.group {
                    CaptureGroup::Named(name) => assert_eq!(name, "timestamp"),
                    _ => panic!("Expected named capture group"),
                }
            }
            _ => panic!("Expected regex transform"),
        }
    }

    #[test]
    fn test_parse_regex_transform_with_on_no_match() {
        let yaml = r#"
name: regex_flow_skip
input: kafka.logs
transforms:
  - regex:
      field: text
      pattern: "test"
      captures:
        match: "0"
      on_no_match: skip
outputs:
  - postgres.logs
"#;
        let parser = Parser::new(".");
        let ir = parser.parse_yaml(yaml).unwrap();

        match &ir.transforms[0] {
            TransformIR::Regex(regex) => {
                assert!(matches!(regex.on_no_match, NoMatchBehavior::Skip));
            }
            _ => panic!("Expected regex transform"),
        }
    }

    #[test]
    fn test_parse_invalid_regex_pattern() {
        let yaml = r#"
name: bad_regex_flow
input: kafka.logs
transforms:
  - regex:
      field: message
      pattern: "[invalid(regex"
      captures:
        match: "0"
outputs:
  - postgres.logs
"#;
        let parser = Parser::new(".");
        let result = parser.parse_yaml(yaml);

        assert!(result.is_err());
    }

    #[test]
    fn test_parse_lookup_transform() {
        let yaml = r#"
name: lookup_flow
input: kafka.orders
transforms:
  - lookup:
      field: country_code
      table: countries
      output: country_name
outputs:
  - postgres.orders
"#;
        let parser = Parser::new(".");
        let ir = parser.parse_yaml(yaml).unwrap();

        match &ir.transforms[0] {
            TransformIR::Lookup(lookup) => {
                assert_eq!(lookup.key_field, "country_code");
                assert_eq!(lookup.table, "countries");
                assert_eq!(lookup.output_field, "country_name");
                assert!(lookup.default.is_none());
            }
            _ => panic!("Expected lookup transform"),
        }
    }

    #[test]
    fn test_parse_lookup_transform_with_default() {
        let yaml = r#"
name: lookup_flow_default
input: kafka.orders
transforms:
  - lookup:
      field: country_code
      table: countries
      output: country_name
      default: "Unknown"
outputs:
  - postgres.orders
"#;
        let parser = Parser::new(".");
        let ir = parser.parse_yaml(yaml).unwrap();

        match &ir.transforms[0] {
            TransformIR::Lookup(lookup) => {
                assert_eq!(lookup.default, Some(serde_json::json!("Unknown")));
            }
            _ => panic!("Expected lookup transform"),
        }
    }

    #[test]
    fn test_parse_filter_transform() {
        let yaml = r#"
name: filter_flow
input: kafka.orders
transforms:
  - filter:
      when: "status == 'active'"
outputs:
  - postgres.orders
"#;
        let parser = Parser::new(".");
        let ir = parser.parse_yaml(yaml).unwrap();

        match &ir.transforms[0] {
            TransformIR::Filter(filter) => match &filter.condition {
                FilterCondition::Expression(expr) => {
                    assert_eq!(expr, "status == 'active'");
                }
                _ => panic!("Expected expression condition"),
            },
            _ => panic!("Expected filter transform"),
        }
    }

    #[test]
    fn test_parse_drop_transform() {
        let yaml = r#"
name: drop_flow
input: kafka.orders
transforms:
  - drop:
      - internal_id
      - secret_key
      - temp_field
outputs:
  - postgres.orders
"#;
        let parser = Parser::new(".");
        let ir = parser.parse_yaml(yaml).unwrap();

        match &ir.transforms[0] {
            TransformIR::Drop(fields) => {
                assert_eq!(fields.len(), 3);
                assert!(fields.contains(&"internal_id".to_string()));
                assert!(fields.contains(&"secret_key".to_string()));
                assert!(fields.contains(&"temp_field".to_string()));
            }
            _ => panic!("Expected drop transform"),
        }
    }

    #[test]
    fn test_parse_coalesce_transform() {
        let yaml = r#"
name: coalesce_flow
input: kafka.orders
transforms:
  - coalesce:
      email:
        - primary_email
        - work_email
        - personal_email
      phone:
        - mobile
        - home_phone
outputs:
  - postgres.orders
"#;
        let parser = Parser::new(".");
        let ir = parser.parse_yaml(yaml).unwrap();

        match &ir.transforms[0] {
            TransformIR::Coalesce(fields) => {
                assert_eq!(fields.len(), 2);

                let email_field = fields.iter().find(|f| f.target == "email").unwrap();
                assert_eq!(email_field.sources.len(), 3);
                assert_eq!(email_field.sources[0], "primary_email");

                let phone_field = fields.iter().find(|f| f.target == "phone").unwrap();
                assert_eq!(phone_field.sources.len(), 2);
            }
            _ => panic!("Expected coalesce transform"),
        }
    }

    #[test]
    fn test_parse_conditional_output() {
        let yaml = r#"
name: conditional_flow
input: kafka.orders
outputs:
  - connector: kafka.high_value
    when: "total > 1000"
"#;
        let parser = Parser::new(".");
        let ir = parser.parse_yaml(yaml).unwrap();

        assert!(ir.outputs[0].condition.is_some());
        match &ir.outputs[0].condition {
            Some(FilterCondition::Expression(expr)) => {
                assert_eq!(expr, "total > 1000");
            }
            _ => panic!("Expected expression condition"),
        }
    }

    #[test]
    fn test_parse_multiple_outputs() {
        let yaml = r#"
name: multi_output_flow
input: kafka.orders
outputs:
  - postgres.all_orders
  - connector: kafka.high_value
    when: "total > 1000"
  - connector: kafka.low_value
    when: "total <= 100"
"#;
        let parser = Parser::new(".");
        let ir = parser.parse_yaml(yaml).unwrap();

        assert_eq!(ir.outputs.len(), 3);
        assert!(ir.outputs[0].condition.is_none());
        assert!(ir.outputs[1].condition.is_some());
        assert!(ir.outputs[2].condition.is_some());
    }

    #[test]
    fn test_parse_multiple_transforms() {
        let yaml = r#"
name: multi_transform_flow
input: kafka.orders
transforms:
  - map:
      customer_id: cust_id
  - drop:
      - internal_field
  - template:
      greeting: "Hello {{ name }}"
outputs:
  - postgres.orders
"#;
        let parser = Parser::new(".");
        let ir = parser.parse_yaml(yaml).unwrap();

        assert_eq!(ir.transforms.len(), 3);
        assert!(matches!(&ir.transforms[0], TransformIR::Map(_)));
        assert!(matches!(&ir.transforms[1], TransformIR::Drop(_)));
        assert!(matches!(&ir.transforms[2], TransformIR::Template(_)));
    }

    #[test]
    fn test_parse_flow_no_transforms() {
        let yaml = r#"
name: passthrough_flow
input: kafka.input
outputs:
  - kafka.output
"#;
        let parser = Parser::new(".");
        let ir = parser.parse_yaml(yaml).unwrap();

        assert_eq!(ir.name, "passthrough_flow");
        assert!(ir.transforms.is_empty());
        assert_eq!(ir.outputs.len(), 1);
    }

    #[test]
    fn test_parse_flow_no_outputs() {
        let yaml = r#"
name: sink_flow
input: kafka.input
transforms:
  - map:
      id: source_id
"#;
        let parser = Parser::new(".");
        let ir = parser.parse_yaml(yaml).unwrap();

        assert_eq!(ir.name, "sink_flow");
        assert!(ir.outputs.is_empty());
    }

    #[test]
    fn test_no_match_behavior_from_string() {
        assert!(matches!(
            NoMatchBehavior::from("skip".to_string()),
            NoMatchBehavior::Skip
        ));
        assert!(matches!(
            NoMatchBehavior::from("SKIP".to_string()),
            NoMatchBehavior::Skip
        ));
        assert!(matches!(
            NoMatchBehavior::from("error".to_string()),
            NoMatchBehavior::Error
        ));
        assert!(matches!(
            NoMatchBehavior::from("ERROR".to_string()),
            NoMatchBehavior::Error
        ));
        assert!(matches!(
            NoMatchBehavior::from("null".to_string()),
            NoMatchBehavior::Null
        ));
        assert!(matches!(
            NoMatchBehavior::from("unknown".to_string()),
            NoMatchBehavior::Null
        ));
        assert!(matches!(
            NoMatchBehavior::from("".to_string()),
            NoMatchBehavior::Null
        ));
    }

    #[test]
    fn test_parse_invalid_yaml() {
        let yaml = "this is not valid yaml: [";
        let parser = Parser::new(".");
        let result = parser.parse_yaml(yaml);

        assert!(result.is_err());
    }

    #[test]
    fn test_parse_missing_required_fields() {
        let yaml = r#"
description: "Missing name and input"
outputs:
  - postgres.output
"#;
        let parser = Parser::new(".");
        let result = parser.parse_yaml(yaml);

        assert!(result.is_err());
    }
}
