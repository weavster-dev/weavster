//! Lookup table transform codegen

use std::collections::HashMap;

use crate::ir::LookupTransform;

/// Helper for generating lookup table code
pub struct LookupCodegen;

impl LookupCodegen {
    /// Generate a static PHF map declaration from a lookup table
    pub fn generate_static_map(name: &str, data: &HashMap<String, String>) -> String {
        let mut code = String::new();

        code.push_str(&format!(
            "static {}: phf::Map<&'static str, &'static str> = phf_map! {{\n",
            name.to_uppercase()
        ));

        for (key, value) in data {
            // Escape strings for Rust
            let escaped_key = Self::escape_string(key);
            let escaped_value = Self::escape_string(value);
            code.push_str(&format!(
                "    \"{}\" => \"{}\",\n",
                escaped_key, escaped_value
            ));
        }

        code.push_str("};\n");
        code
    }

    /// Generate code for a lookup transform
    pub fn generate(lookup: &LookupTransform) -> String {
        let table_name = lookup.table.to_uppercase();

        let mut code = String::new();

        code.push_str(&format!(
            r#"    if let Some(key) = source["{}"].as_str() {{
        match {}.get(key) {{
            Some(value) => {{
                output.insert("{}".into(), Value::String((*value).into()));
            }}
"#,
            lookup.key_field, table_name, lookup.output_field
        ));

        // Handle default value or missing key
        if let Some(default) = &lookup.default {
            code.push_str(&format!(
                r#"            None => {{
                output.insert("{}".into(), serde_json::json!({}));
            }}
"#,
                lookup.output_field, default
            ));
        } else {
            code.push_str(
                r#"            None => {
                // Key not found, skip this field
            }
"#,
            );
        }

        code.push_str("        }\n");
        code.push_str("    }\n");

        code
    }

    /// Load a lookup table from a CSV file
    pub fn load_csv(
        path: &std::path::Path,
        key_column: Option<&str>,
        value_column: Option<&str>,
    ) -> Result<HashMap<String, String>, String> {
        let content =
            std::fs::read_to_string(path).map_err(|e| format!("Failed to read CSV: {}", e))?;

        let mut reader = csv::Reader::from_reader(content.as_bytes());
        let headers = reader
            .headers()
            .map_err(|e| format!("Failed to read CSV headers: {}", e))?
            .clone();

        // Determine key and value column indices
        let key_idx = match key_column {
            Some(name) => headers
                .iter()
                .position(|h| h == name)
                .ok_or_else(|| format!("Key column '{}' not found", name))?,
            None => 0, // Default to first column
        };

        let value_idx = match value_column {
            Some(name) => headers
                .iter()
                .position(|h| h == name)
                .ok_or_else(|| format!("Value column '{}' not found", name))?,
            None => 1, // Default to second column
        };

        let mut map = HashMap::new();

        for result in reader.records() {
            let record = result.map_err(|e| format!("CSV parse error: {}", e))?;

            let key = record
                .get(key_idx)
                .ok_or_else(|| "Missing key column".to_string())?;
            let value = record
                .get(value_idx)
                .ok_or_else(|| "Missing value column".to_string())?;

            map.insert(key.to_string(), value.to_string());
        }

        Ok(map)
    }

    /// Escape a string for embedding in Rust source
    fn escape_string(s: &str) -> String {
        s.replace('\\', "\\\\")
            .replace('"', "\\\"")
            .replace('\n', "\\n")
            .replace('\r', "\\r")
            .replace('\t', "\\t")
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_generate_static_map() {
        let mut data = HashMap::new();
        data.insert("US".to_string(), "United States".to_string());
        data.insert("GB".to_string(), "United Kingdom".to_string());

        let code = LookupCodegen::generate_static_map("country_names", &data);

        assert!(code.contains("COUNTRY_NAMES"));
        assert!(code.contains("phf_map!"));
        assert!(code.contains("\"US\" => \"United States\""));
    }

    #[test]
    fn test_escape_string() {
        let escaped = LookupCodegen::escape_string("Hello \"World\"\nNew Line");
        assert_eq!(escaped, "Hello \\\"World\\\"\\nNew Line");
    }
}
