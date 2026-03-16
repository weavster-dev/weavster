use serde_json::Value;
fn check<T: PartialEq>(_: T) {}
fn main() { check(Value::Null); }
