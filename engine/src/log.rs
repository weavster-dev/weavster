//! Structured logs (Engine Plan E3 slice 3): one JSON object per line on
//! stderr, with pipeline/document/stage fields. Deliberately framework-free
//! (just serde_json, already a dependency); a tracing stack can replace this
//! when the engine grows subscribers.

use serde_json::json;

pub fn done(pipeline: &str, document: usize) {
    emit(
        json!({ "level": "info", "event": "document", "pipeline": pipeline, "document": document, "status": "ok" }),
    );
}

pub fn error(pipeline: &str, document: usize, stage: &str, error_type: &str, message: &str) {
    emit(
        json!({ "level": "error", "event": "document", "pipeline": pipeline, "document": document, "stage": stage, "type": error_type, "message": message }),
    );
}

fn emit(record: serde_json::Value) {
    eprintln!("{record}");
}
