//! Flow definition and orchestration
//!
//! A flow defines a data pipeline with one input and multiple outputs.

use serde::{Deserialize, Serialize};
use std::collections::HashMap;

use crate::transforms::TransformConfig;

/// A flow definition
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Flow {
    /// Flow name (must be unique within project)
    pub name: String,

    /// Optional description
    #[serde(default)]
    pub description: Option<String>,

    /// Input connector reference
    pub input: String,

    /// Transform pipeline
    #[serde(default)]
    pub transforms: Vec<TransformConfig>,

    /// Output connector references with optional filters
    #[serde(default)]
    pub outputs: Vec<OutputConfig>,

    /// Flow-level variables
    #[serde(default)]
    pub vars: HashMap<String, serde_yaml::Value>,
}

/// Output configuration
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(untagged)]
pub enum OutputConfig {
    /// Simple output reference
    Simple(String),

    /// Output with filter condition
    Conditional {
        /// Connector reference
        connector: String,
        /// Filter expression
        when: String,
    },
}

impl Flow {
    /// Get the flow name
    pub fn name(&self) -> &str {
        &self.name
    }

    /// Get output connector names
    pub fn output_connectors(&self) -> Vec<&str> {
        self.outputs
            .iter()
            .map(|o| match o {
                OutputConfig::Simple(s) => s.as_str(),
                OutputConfig::Conditional { connector, .. } => connector.as_str(),
            })
            .collect()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_simple_flow() {
        let yaml = r#"
name: test_flow
input: kafka.orders
outputs:
  - postgres.processed_orders
"#;
        let flow: Flow = serde_yaml::from_str(yaml).unwrap();
        assert_eq!(flow.name, "test_flow");
        assert_eq!(flow.input, "kafka.orders");
        assert_eq!(flow.outputs.len(), 1);
    }

    #[test]
    fn test_parse_flow_with_conditional_output() {
        let yaml = r#"
name: test_flow
input: kafka.orders
outputs:
  - connector: kafka.high_value
    when: "total > 1000"
"#;
        let flow: Flow = serde_yaml::from_str(yaml).unwrap();
        assert_eq!(flow.outputs.len(), 1);
        match &flow.outputs[0] {
            OutputConfig::Conditional { connector, when } => {
                assert_eq!(connector, "kafka.high_value");
                assert_eq!(when, "total > 1000");
            }
            _ => panic!("Expected conditional output"),
        }
    }
}
