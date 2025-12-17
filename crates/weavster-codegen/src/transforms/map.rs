//! Field mapping transform codegen

use crate::ir::FieldMapping;

/// Helper for generating field mapping code
pub struct MapCodegen;

impl MapCodegen {
    /// Generate code for a batch of field mappings
    pub fn generate(mappings: &[FieldMapping]) -> String {
        let mut code = String::new();

        for mapping in mappings {
            code.push_str(&Self::generate_single(mapping));
        }

        code
    }

    /// Generate code for a single field mapping
    pub fn generate_single(mapping: &FieldMapping) -> String {
        let source_access = Self::path_to_accessor(&mapping.source);

        match &mapping.default {
            Some(default) => {
                format!(
                    r#"    output.insert("{target}".into(),
        source{source}.cloned().unwrap_or_else(|| serde_json::json!({default})));
"#,
                    target = mapping.target,
                    source = source_access,
                    default = default,
                )
            }
            None => {
                format!(
                    r#"    if let Some(v) = source{source} {{
        output.insert("{target}".into(), v.clone());
    }}
"#,
                    target = mapping.target,
                    source = source_access,
                )
            }
        }
    }

    /// Convert a dot-notation path to Rust JSON accessor
    fn path_to_accessor(path: &str) -> String {
        path.split('.')
            .map(|part| format!("[\"{}\"]", part))
            .collect()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_simple_mapping() {
        let mapping = FieldMapping {
            target: "customer_id".to_string(),
            source: "cust_id".to_string(),
            default: None,
        };

        let code = MapCodegen::generate_single(&mapping);
        assert!(code.contains("customer_id"));
        assert!(code.contains(r#"["cust_id"]"#));
    }

    #[test]
    fn test_nested_path() {
        let mapping = FieldMapping {
            target: "id".to_string(),
            source: "customer.account.id".to_string(),
            default: None,
        };

        let code = MapCodegen::generate_single(&mapping);
        assert!(code.contains(r#"["customer"]["account"]["id"]"#));
    }

    #[test]
    fn test_with_default() {
        let mapping = FieldMapping {
            target: "status".to_string(),
            source: "order_status".to_string(),
            default: Some(serde_json::json!("pending")),
        };

        let code = MapCodegen::generate_single(&mapping);
        assert!(code.contains("unwrap_or_else"));
        assert!(code.contains("pending"));
    }
}
