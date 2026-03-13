#![allow(missing_docs)]
// schema.rs

diesel::table! {
    bridge_messages (id) {
        id -> Integer,
        bridge_name -> Text,
        message_id -> Text,
        payload -> Binary,
        created_at -> Timestamp,
        processed_at -> Nullable<Timestamp>,
        status -> Text,
        retry_count -> Integer,
        error_message -> Nullable<Text>,
    }
}

diesel::table! {
    flow_executions (id) {
        id -> Integer,
        flow_name -> Text,
        started_at -> Timestamp,
        completed_at -> Nullable<Timestamp>,
        status -> Text,
        records_processed -> Integer,
        records_failed -> Integer,
        error_message -> Nullable<Text>,
    }
}

diesel::table! {
    processed_files (id) {
        id -> Integer,
        flow_name -> Text,
        file_path -> Text,
        file_hash -> Text,
        processed_at -> Timestamp,
        record_count -> Integer,
        status -> Text,
        error_message -> Nullable<Text>,
    }
}

diesel::table! {
    test_results (id) {
        id -> Integer,
        test_name -> Text,
        flow_name -> Text,
        executed_at -> Timestamp,
        status -> Text,
        duration_ms -> Integer,
        error_message -> Nullable<Text>,
        diff -> Nullable<Text>,
    }
}

diesel::allow_tables_to_appear_in_same_query!(
    bridge_messages,
    flow_executions,
    processed_files,
    test_results,
);
