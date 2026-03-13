use serde_json::Value;
fn check<T: Eq>(_: T) {}
fn main() { check(Value::Null); }
