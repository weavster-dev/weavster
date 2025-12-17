//! Jinja template transform codegen

use crate::ir::TemplateField;

/// Helper for generating template transform code
pub struct TemplateCodegen;

impl TemplateCodegen {
    /// Generate code for template transforms
    pub fn generate(templates: &[TemplateField]) -> String {
        let mut code = String::new();

        code.push_str("    {\n");
        code.push_str("        let env = Environment::new();\n");

        for tpl in templates {
            // Escape the template string for embedding in Rust
            let escaped = Self::escape_template(&tpl.template);

            code.push_str(&format!(
                "        match env.render_str(r#\"{}\"#, &source) {{\n            Ok(rendered) => {{\n                output.insert(\"{}\".into(), Value::String(rendered));\n            }}\n            Err(e) => {{\n                // Template render failed, skip this field\n                // In production, might want to log this\n            }}\n        }}\n",
                escaped,
                tpl.target,
            ));
        }

        code.push_str("    }\n");
        code
    }

    /// Escape a template string for embedding in Rust raw string literal
    fn escape_template(template: &str) -> String {
        // For raw strings, we just need to handle the delimiter
        // If the template contains "#, we might need a different approach
        template.replace("\"#", "\\\"#")
    }

    /// Validate a template at compile time
    pub fn validate(template: &str) -> Result<(), String> {
        let env = minijinja::Environment::new();
        env.render_str(template, minijinja::context!())
            .map(|_| ())
            .map_err(|e| e.to_string())
    }

    /// Extract variables used in a template (for documentation/validation)
    pub fn extract_variables(template: &str) -> Vec<String> {
        let re = regex::Regex::new(r"\{\{\s*(\w+)").unwrap();
        re.captures_iter(template)
            .filter_map(|cap| cap.get(1).map(|m| m.as_str().to_string()))
            .collect()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_generate_simple_template() {
        let templates = vec![TemplateField {
            target: "greeting".to_string(),
            template: "Hello {{ name }}!".to_string(),
        }];

        let code = TemplateCodegen::generate(&templates);
        assert!(code.contains("greeting"));
        assert!(code.contains("Environment::new()"));
    }

    #[test]
    fn test_extract_variables() {
        let vars = TemplateCodegen::extract_variables("{{ first_name }} {{ last_name }}");
        assert!(vars.contains(&"first_name".to_string()));
        assert!(vars.contains(&"last_name".to_string()));
    }

    #[test]
    fn test_validate_valid_template() {
        assert!(TemplateCodegen::validate("Hello {{ name }}!").is_ok());
    }
}
