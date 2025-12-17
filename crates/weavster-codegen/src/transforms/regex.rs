//! Regex transform codegen

use crate::ir::{CaptureGroup, RegexTransform};

/// Helper for generating regex transform code
pub struct RegexCodegen;

impl RegexCodegen {
    /// Generate static regex declaration
    pub fn generate_static(index: usize, pattern: &str) -> String {
        format!(
            "static REGEX_{}: Lazy<Regex> = Lazy::new(|| {{\n    Regex::new(r#\"{}\"#).expect(\"Invalid regex pattern\")\n}});\n",
            index, pattern
        )
    }

    /// Generate code for a regex transform
    pub fn generate(index: usize, regex: &RegexTransform) -> String {
        let mut code = String::new();

        // Open source field check
        code.push_str(&format!(
            r#"    if let Some(text) = source["{}"].as_str() {{
        if let Some(caps) = REGEX_{}.captures(text) {{
"#,
            regex.source_field, index
        ));

        // Generate capture extractions
        for (output_field, capture) in &regex.captures {
            let capture_expr = match &capture.group {
                CaptureGroup::Index(i) => format!("caps.get({})", i),
                CaptureGroup::Named(name) => format!("caps.name(\"{}\")", name),
            };

            code.push_str(&format!(
                r#"            if let Some(m) = {} {{
                output.insert("{}".into(), Value::String(m.as_str().into()));
            }}
"#,
                capture_expr, output_field
            ));
        }

        // Close blocks
        code.push_str("        }\n");
        code.push_str("    }\n");

        code
    }

    /// Validate a regex pattern at compile time
    pub fn validate(pattern: &str) -> Result<(), String> {
        regex::Regex::new(pattern)
            .map(|_| ())
            .map_err(|e| e.to_string())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_generate_static() {
        let code = RegexCodegen::generate_static(0, r"^\d{3}-\d{4}$");
        assert!(code.contains("REGEX_0"));
        assert!(code.contains("Lazy::new"));
    }

    #[test]
    fn test_validate_valid_pattern() {
        assert!(RegexCodegen::validate(r"^\d+$").is_ok());
    }

    #[test]
    fn test_validate_invalid_pattern() {
        assert!(RegexCodegen::validate(r"[invalid").is_err());
    }
}
