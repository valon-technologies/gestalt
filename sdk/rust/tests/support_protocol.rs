use prost_types::{ListValue, Struct, Value, value::Kind};

pub fn json_from_struct(value: &Struct) -> serde_json::Value {
    serde_json::Value::Object(
        value
            .fields
            .iter()
            .map(|(key, value)| (key.clone(), json_from_value(value)))
            .collect(),
    )
}

fn json_from_value(value: &Value) -> serde_json::Value {
    match &value.kind {
        Some(Kind::NullValue(_)) | None => serde_json::Value::Null,
        Some(Kind::BoolValue(value)) => serde_json::Value::Bool(*value),
        Some(Kind::NumberValue(value)) => serde_json::json!(*value),
        Some(Kind::StringValue(value)) => serde_json::Value::String(value.clone()),
        Some(Kind::ListValue(ListValue { values })) => {
            serde_json::Value::Array(values.iter().map(json_from_value).collect())
        }
        Some(Kind::StructValue(value)) => json_from_struct(value),
    }
}
