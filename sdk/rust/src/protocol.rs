use std::time::{Duration, SystemTime, UNIX_EPOCH};

use prost_types::{ListValue, Struct, Timestamp, Value, value::Kind};

use crate::{Error, Result};

const NANOS_PER_SECOND: i128 = 1_000_000_000;

/// Converts a JSON object into a protobuf `Struct`.
pub fn struct_from_json(value: serde_json::Value) -> Result<Struct> {
    match value {
        serde_json::Value::Object(fields) => Ok(struct_from_map(fields)),
        _ => Err(Error::bad_request(
            "expected JSON object when building protobuf Struct",
        )),
    }
}

/// Converts a JSON object map into a protobuf `Struct`.
pub fn struct_from_map(fields: serde_json::Map<String, serde_json::Value>) -> Struct {
    Struct {
        fields: fields
            .into_iter()
            .map(|(key, value)| (key, value_from_json(value)))
            .collect(),
    }
}

/// Converts a protobuf `Struct` into a JSON object.
pub fn json_from_struct(value: &Struct) -> serde_json::Value {
    serde_json::Value::Object(
        value
            .fields
            .iter()
            .map(|(key, value)| (key.clone(), json_from_value(value)))
            .collect(),
    )
}

/// Converts a JSON value into a protobuf `Value`.
pub fn value_from_json(value: serde_json::Value) -> Value {
    let kind = match value {
        serde_json::Value::Null => Kind::NullValue(0),
        serde_json::Value::Bool(value) => Kind::BoolValue(value),
        serde_json::Value::Number(value) => Kind::NumberValue(value.as_f64().unwrap_or(0.0)),
        serde_json::Value::String(value) => Kind::StringValue(value),
        serde_json::Value::Array(values) => Kind::ListValue(ListValue {
            values: values.into_iter().map(value_from_json).collect(),
        }),
        serde_json::Value::Object(fields) => Kind::StructValue(struct_from_map(fields)),
    };
    Value { kind: Some(kind) }
}

/// Converts a protobuf `Value` into a JSON value.
pub fn json_from_value(value: &Value) -> serde_json::Value {
    match &value.kind {
        Some(Kind::NullValue(_)) | None => serde_json::Value::Null,
        Some(Kind::BoolValue(value)) => serde_json::Value::Bool(*value),
        Some(Kind::NumberValue(value)) => serde_json::json!(*value),
        Some(Kind::StringValue(value)) => serde_json::Value::String(value.clone()),
        Some(Kind::ListValue(value)) => {
            serde_json::Value::Array(value.values.iter().map(json_from_value).collect())
        }
        Some(Kind::StructValue(value)) => json_from_struct(value),
    }
}

/// Converts a `SystemTime` into a protobuf `Timestamp`.
pub fn timestamp_from_system_time(value: SystemTime) -> Timestamp {
    let total_nanos = match value.duration_since(UNIX_EPOCH) {
        Ok(duration) => duration_to_nanos(duration),
        Err(err) => -duration_to_nanos(err.duration()),
    };
    Timestamp {
        seconds: (total_nanos.div_euclid(NANOS_PER_SECOND)) as i64,
        nanos: (total_nanos.rem_euclid(NANOS_PER_SECOND)) as i32,
    }
}

/// Converts a protobuf `Timestamp` into `SystemTime`.
pub fn system_time_from_timestamp(value: &Timestamp) -> Result<SystemTime> {
    if !(0..1_000_000_000).contains(&value.nanos) {
        return Err(Error::bad_request("protobuf Timestamp nanos out of range"));
    }
    let total_nanos = (value.seconds as i128) * NANOS_PER_SECOND + i128::from(value.nanos);
    let duration = nanos_to_duration(total_nanos.unsigned_abs())?;
    if total_nanos >= 0 {
        UNIX_EPOCH
            .checked_add(duration)
            .ok_or_else(|| Error::bad_request("protobuf Timestamp seconds out of range"))
    } else {
        UNIX_EPOCH
            .checked_sub(duration)
            .ok_or_else(|| Error::bad_request("protobuf Timestamp seconds out of range"))
    }
}

fn duration_to_nanos(duration: Duration) -> i128 {
    (duration.as_secs() as i128) * NANOS_PER_SECOND + i128::from(duration.subsec_nanos())
}

fn nanos_to_duration(nanos: u128) -> Result<Duration> {
    let seconds = nanos / (NANOS_PER_SECOND as u128);
    let subnanos = nanos % (NANOS_PER_SECOND as u128);
    Ok(Duration::new(
        seconds
            .try_into()
            .map_err(|_| Error::bad_request("protobuf Timestamp seconds out of range"))?,
        subnanos as u32,
    ))
}
