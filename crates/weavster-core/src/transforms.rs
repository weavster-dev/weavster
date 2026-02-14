//! Transform DSL configuration
//!
//! Transforms modify messages as they flow through a pipeline.
//! Transform configurations are parsed here, then compiled to WASM by weavster-codegen.
//!
//! # Built-in Transforms
//!
//! - `map` - Direct field mapping
//! - `regex` - Pattern matching and extraction
//! - `template` - Jinja template rendering
//! - `lookup` - Translation table lookups
//! - `filter` - Include/exclude messages based on conditions
//! - `drop` - Remove fields from messages
//! - `coalesce` - Use first non-null value
//!
//! # Example
//!
//! ```yaml
//! transforms:
//!   - map:
//!       customer_id: source.cust_id
//!       order_total: source.total
//!
//!   - template:
//!       full_name: "{{ first_name }} {{ last_name }}"
//!
//!   - filter:
//!       when: "total > 100"
//! ```

use serde::{Deserialize, Serialize};
use std::collections::HashMap;

use crate::config::ErrorHandlingConfig;

/// Regex transform configuration
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RegexConfig {
    /// Source field to match against
    pub field: String,
    /// Regex pattern (with optional named capture groups)
    pub pattern: String,
    /// Capture group mappings: output_field -> group (index or name)
    pub captures: HashMap<String, String>,
    /// Behavior when pattern doesn't match: null, skip, or error
    #[serde(default)]
    pub on_no_match: Option<String>,
}

/// Lookup transform configuration
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LookupConfig {
    /// Source field containing the lookup key
    pub field: String,
    /// Name of the lookup table artifact
    pub table: String,
    /// Output field name
    pub output: String,
    /// Default value if key not found
    #[serde(default)]
    pub default: Option<serde_json::Value>,
}

/// Filter transform configuration
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct FilterConfig {
    /// Condition expression (compiled to WASM)
    pub when: String,
}

/// Transform configuration from YAML
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(untagged)]
pub enum TransformConfig {
    /// Direct field mapping: target <- source path
    Map {
        /// Field mappings
        map: HashMap<String, String>,
        /// Transform-level error handling override
        #[serde(default, skip_serializing_if = "Option::is_none")]
        error_handling: Option<ErrorHandlingConfig>,
    },

    /// Regex pattern matching and capture extraction
    Regex {
        /// Regex configuration
        regex: RegexConfig,
        /// Transform-level error handling override
        #[serde(default, skip_serializing_if = "Option::is_none")]
        error_handling: Option<ErrorHandlingConfig>,
    },

    /// Jinja template rendering
    Template {
        /// Template mappings
        template: HashMap<String, String>,
        /// Transform-level error handling override
        #[serde(default, skip_serializing_if = "Option::is_none")]
        error_handling: Option<ErrorHandlingConfig>,
    },

    /// Lookup table reference
    Lookup {
        /// Lookup configuration
        lookup: LookupConfig,
        /// Transform-level error handling override
        #[serde(default, skip_serializing_if = "Option::is_none")]
        error_handling: Option<ErrorHandlingConfig>,
    },

    /// Filter messages based on condition
    Filter {
        /// Filter configuration
        filter: FilterConfig,
        /// Transform-level error handling override
        #[serde(default, skip_serializing_if = "Option::is_none")]
        error_handling: Option<ErrorHandlingConfig>,
    },

    /// Drop specified fields
    Drop {
        /// Fields to drop
        drop: Vec<String>,
        /// Transform-level error handling override
        #[serde(default, skip_serializing_if = "Option::is_none")]
        error_handling: Option<ErrorHandlingConfig>,
    },

    /// Coalesce: use first non-null value from list
    Coalesce {
        /// Coalesce mappings
        coalesce: HashMap<String, Vec<String>>,
        /// Transform-level error handling override
        #[serde(default, skip_serializing_if = "Option::is_none")]
        error_handling: Option<ErrorHandlingConfig>,
    },

    /// Add static fields
    AddFields {
        /// Fields to add
        add_fields: HashMap<String, serde_json::Value>,
        /// Transform-level error handling override
        #[serde(default, skip_serializing_if = "Option::is_none")]
        error_handling: Option<ErrorHandlingConfig>,
    },
}

impl TransformConfig {
    /// Get the optional transform-level error handling configuration
    pub fn error_handling(&self) -> Option<&ErrorHandlingConfig> {
        match self {
            Self::Map { error_handling, .. }
            | Self::Regex { error_handling, .. }
            | Self::Template { error_handling, .. }
            | Self::Lookup { error_handling, .. }
            | Self::Filter { error_handling, .. }
            | Self::Drop { error_handling, .. }
            | Self::Coalesce { error_handling, .. }
            | Self::AddFields { error_handling, .. } => error_handling.as_ref(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_map_transform() {
        let yaml = r#"
map:
  customer_id: cust_id
  order_total: total
"#;
        let config: TransformConfig = serde_yaml::from_str(yaml).unwrap();
        match config {
            TransformConfig::Map { map: mappings, .. } => {
                assert_eq!(mappings.get("customer_id"), Some(&"cust_id".to_string()));
            }
            _ => panic!("Expected Map transform"),
        }
    }

    #[test]
    fn test_parse_regex_transform() {
        let yaml = r#"
regex:
  field: phone
  pattern: '^\+?(\d{1,3})?'
  captures:
    country_code: "1"
"#;
        let config: TransformConfig = serde_yaml::from_str(yaml).unwrap();
        match config {
            TransformConfig::Regex { regex, .. } => {
                assert_eq!(regex.field, "phone");
                assert!(regex.pattern.contains(r"\d"));
                assert!(regex.captures.contains_key("country_code"));
            }
            _ => panic!("Expected Regex transform"),
        }
    }

    #[test]
    fn test_parse_template_transform() {
        let yaml = r#"
template:
  full_name: "{{ first_name }} {{ last_name }}"
  greeting: "Hello {{ title }}. {{ last_name }}"
"#;
        let config: TransformConfig = serde_yaml::from_str(yaml).unwrap();
        match config {
            TransformConfig::Template {
                template: templates,
                ..
            } => {
                assert!(templates.contains_key("full_name"));
                assert!(templates.get("greeting").unwrap().contains("Hello"));
            }
            _ => panic!("Expected Template transform"),
        }
    }

    #[test]
    fn test_parse_lookup_transform() {
        let yaml = r#"
lookup:
  field: country_code
  table: country_names
  output: country_name
  default: "Unknown"
"#;
        let config: TransformConfig = serde_yaml::from_str(yaml).unwrap();
        match config {
            TransformConfig::Lookup { lookup, .. } => {
                assert_eq!(lookup.field, "country_code");
                assert_eq!(lookup.table, "country_names");
                assert_eq!(lookup.output, "country_name");
                assert!(lookup.default.is_some());
            }
            _ => panic!("Expected Lookup transform"),
        }
    }

    #[test]
    fn test_parse_filter_transform() {
        let yaml = r#"
filter:
  when: "total > 100 && status == 'active'"
"#;
        let config: TransformConfig = serde_yaml::from_str(yaml).unwrap();
        match config {
            TransformConfig::Filter { filter, .. } => {
                assert!(filter.when.contains("total > 100"));
            }
            _ => panic!("Expected Filter transform"),
        }
    }

    #[test]
    fn test_parse_drop_transform() {
        let yaml = r#"
drop:
  - internal_id
  - debug_info
  - temp_field
"#;
        let config: TransformConfig = serde_yaml::from_str(yaml).unwrap();
        match config {
            TransformConfig::Drop { drop: fields, .. } => {
                assert_eq!(fields.len(), 3);
                assert!(fields.contains(&"internal_id".to_string()));
            }
            _ => panic!("Expected Drop transform"),
        }
    }

    #[test]
    fn test_parse_coalesce_transform() {
        let yaml = r#"
coalesce:
  email:
    - primary_email
    - secondary_email
    - backup_email
"#;
        let config: TransformConfig = serde_yaml::from_str(yaml).unwrap();
        match config {
            TransformConfig::Coalesce {
                coalesce: mappings, ..
            } => {
                let sources = mappings.get("email").unwrap();
                assert_eq!(sources.len(), 3);
                assert_eq!(sources[0], "primary_email");
            }
            _ => panic!("Expected Coalesce transform"),
        }
    }

    #[test]
    fn test_parse_transform_with_error_handling() {
        let yaml = r#"
map:
  customer_id: cust_id
error_handling:
  on_error: stop_on_error
  log_level: warn
"#;
        let config: TransformConfig = serde_yaml::from_str(yaml).unwrap();
        match config {
            TransformConfig::Map { error_handling, .. } => {
                let eh = error_handling.unwrap();
                assert_eq!(eh.on_error, crate::config::OnErrorBehavior::StopOnError);
                assert_eq!(eh.log_level, "warn");
            }
            _ => panic!("Expected Map transform"),
        }
    }

    #[test]
    fn test_parse_transform_without_error_handling() {
        let yaml = r#"
drop:
  - temp_field
"#;
        let config: TransformConfig = serde_yaml::from_str(yaml).unwrap();
        assert!(config.error_handling().is_none());
    }
}
