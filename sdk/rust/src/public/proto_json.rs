//! Protobuf-JSON encoding helpers for the public REST transport.

#![allow(dead_code)]

use std::time::{Duration, UNIX_EPOCH};

use base64::Engine as _;
use base64::engine::general_purpose::STANDARD as BASE64;
use prost_types::Timestamp;

use crate::rpc_support::{GestaltError, gestalt_error_code};

pub(crate) fn encode_i64(value: i64) -> serde_json::Value {
    serde_json::Value::String(value.to_string())
}

pub(crate) fn decode_i64(value: &serde_json::Value) -> Result<i64, GestaltError> {
    match value {
        serde_json::Value::String(text) => text.parse().map_err(invalid_argument),
        serde_json::Value::Number(number) => number
            .as_i64()
            .ok_or_else(|| invalid_argument("int64 out of range")),
        _ => Err(invalid_argument("expected int64")),
    }
}

pub(crate) fn encode_u64(value: u64) -> serde_json::Value {
    serde_json::Value::String(value.to_string())
}

pub(crate) fn decode_u64(value: &serde_json::Value) -> Result<u64, GestaltError> {
    match value {
        serde_json::Value::String(text) => text.parse().map_err(invalid_argument),
        serde_json::Value::Number(number) => number
            .as_u64()
            .ok_or_else(|| invalid_argument("uint64 out of range")),
        _ => Err(invalid_argument("expected uint64")),
    }
}

pub(crate) fn encode_bytes(value: &[u8]) -> serde_json::Value {
    serde_json::Value::String(BASE64.encode(value))
}

pub(crate) fn decode_bytes(value: &serde_json::Value) -> Result<Vec<u8>, GestaltError> {
    let text = value
        .as_str()
        .ok_or_else(|| invalid_argument("expected base64 bytes"))?;
    BASE64
        .decode(text)
        .map_err(|err| invalid_argument(err.to_string()))
}

pub(crate) fn encode_timestamp(value: &Timestamp) -> serde_json::Value {
    serde_json::Value::String(format_timestamp(value))
}

pub(crate) fn decode_timestamp(value: &serde_json::Value) -> Result<Timestamp, GestaltError> {
    let text = value
        .as_str()
        .ok_or_else(|| invalid_argument("expected RFC3339 timestamp"))?;
    parse_timestamp(text)
}

pub(crate) fn encode_duration(value: &prost_types::Duration) -> serde_json::Value {
    let seconds = value.seconds;
    let nanos = value.nanos.clamp(0, 999_999_999);
    if nanos == 0 {
        return serde_json::Value::String(format!("{seconds}s"));
    }
    let fractional = format!("{:09}", nanos).trim_end_matches('0').to_string();
    serde_json::Value::String(format!("{seconds}.{fractional}s"))
}

pub(crate) fn decode_duration(
    value: &serde_json::Value,
) -> Result<prost_types::Duration, GestaltError> {
    let text = value
        .as_str()
        .ok_or_else(|| invalid_argument("expected duration string"))?;
    parse_duration(text)
}

fn format_timestamp(value: &Timestamp) -> String {
    let nanos = value.nanos.clamp(0, 999_999_999) as u32;
    if value.seconds >= 0 {
        let time = UNIX_EPOCH + Duration::new(value.seconds as u64, nanos);
        let datetime: chrono::DateTime<chrono::Utc> = time.into();
        return datetime.to_rfc3339_opts(chrono::SecondsFormat::AutoSi, true);
    }
    let time = UNIX_EPOCH
        .checked_sub(Duration::new(value.seconds.unsigned_abs(), 0))
        .and_then(|base| base.checked_sub(Duration::new(0, nanos)))
        .unwrap_or(UNIX_EPOCH);
    let datetime: chrono::DateTime<chrono::Utc> = time.into();
    datetime.to_rfc3339_opts(chrono::SecondsFormat::AutoSi, true)
}

fn parse_timestamp(text: &str) -> Result<Timestamp, GestaltError> {
    let parsed = chrono::DateTime::parse_from_rfc3339(text)
        .map_err(|err| invalid_argument(err.to_string()))?;
    let utc = parsed.with_timezone(&chrono::Utc);
    Ok(Timestamp {
        seconds: utc.timestamp(),
        nanos: utc.timestamp_subsec_nanos() as i32,
    })
}

fn parse_duration(text: &str) -> Result<prost_types::Duration, GestaltError> {
    let trimmed = text.trim();
    if !trimmed.ends_with('s') {
        return Err(invalid_argument("duration must end with s"));
    }
    let body = &trimmed[..trimmed.len() - 1];
    let (seconds_text, nanos) = if let Some((whole, fractional)) = body.split_once('.') {
        let seconds: i64 = whole.parse().map_err(invalid_argument)?;
        let padded = format!("{fractional:0<9}");
        let nanos: i32 = padded[..9.min(padded.len())]
            .parse()
            .map_err(invalid_argument)?;
        (seconds, nanos)
    } else {
        (body.parse().map_err(invalid_argument)?, 0)
    };
    Ok(prost_types::Duration {
        seconds: seconds_text,
        nanos,
    })
}

fn invalid_argument(message: impl ToString) -> GestaltError {
    GestaltError::new(gestalt_error_code::INVALID_ARGUMENT, message.to_string())
}

pub(crate) fn invalid_proto_json(message: impl ToString) -> GestaltError {
    invalid_argument(message)
}

pub(crate) fn encode_struct(value: &prost_types::Struct) -> serde_json::Value {
    let mut object = serde_json::Map::new();
    for (key, field) in &value.fields {
        object.insert(key.clone(), encode_value(field));
    }
    serde_json::Value::Object(object)
}

pub(crate) fn decode_struct(
    value: &serde_json::Value,
) -> Result<prost_types::Struct, GestaltError> {
    let Some(object) = value.as_object() else {
        return Err(invalid_argument("expected struct object"));
    };
    let mut fields = std::collections::BTreeMap::new();
    for (key, field) in object {
        fields.insert(key.clone(), decode_value(field)?);
    }
    Ok(prost_types::Struct { fields })
}

pub(crate) fn encode_value(value: &prost_types::Value) -> serde_json::Value {
    use prost_types::value::Kind;
    match &value.kind {
        Some(Kind::NullValue(_)) => serde_json::Value::Null,
        Some(Kind::NumberValue(number)) => serde_json::json!(*number),
        Some(Kind::StringValue(text)) => serde_json::Value::String(text.clone()),
        Some(Kind::BoolValue(flag)) => serde_json::Value::Bool(*flag),
        Some(Kind::StructValue(inner)) => encode_struct(inner),
        Some(Kind::ListValue(list)) => {
            serde_json::Value::Array(list.values.iter().map(encode_value).collect())
        }
        None => serde_json::Value::Null,
    }
}

pub(crate) fn decode_value(value: &serde_json::Value) -> Result<prost_types::Value, GestaltError> {
    use prost_types::value::Kind;
    let kind = match value {
        serde_json::Value::Null => Kind::NullValue(0),
        serde_json::Value::Bool(flag) => Kind::BoolValue(*flag),
        serde_json::Value::Number(number) => Kind::NumberValue(number.as_f64().unwrap_or(0.0)),
        serde_json::Value::String(text) => Kind::StringValue(text.clone()),
        serde_json::Value::Array(items) => Kind::ListValue(prost_types::ListValue {
            values: items
                .iter()
                .map(decode_value)
                .collect::<Result<Vec<_>, _>>()?,
        }),
        serde_json::Value::Object(entries) => {
            let mut fields = std::collections::BTreeMap::new();
            for (key, field) in entries {
                fields.insert(key.clone(), decode_value(field)?);
            }
            Kind::StructValue(prost_types::Struct { fields })
        }
    };
    Ok(prost_types::Value { kind: Some(kind) })
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::generated::v1;
    use crate::public::generated::codec::app::{
        decode_wire_operation_annotations_json, encode_wire_operation_annotations_json,
    };

    #[test]
    fn operation_annotations_round_trip_optional_bools() {
        let wire = v1::OperationAnnotations {
            read_only_hint: Some(true),
            idempotent_hint: Some(false),
            destructive_hint: None,
            open_world_hint: Some(true),
        };
        let json = encode_wire_operation_annotations_json(&wire);
        let decoded = decode_wire_operation_annotations_json(&json).expect("decode wire JSON");
        assert_eq!(decoded.read_only_hint, Some(true));
        assert_eq!(decoded.destructive_hint, None);
    }

    #[test]
    fn int64_round_trip() {
        let encoded = encode_i64(42);
        let decoded = decode_i64(&encoded).expect("decode int64");
        assert_eq!(decoded, 42);
    }
}
