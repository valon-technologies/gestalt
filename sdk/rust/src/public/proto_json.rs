#![allow(dead_code)]

use std::time::{Duration, UNIX_EPOCH};

use base64::Engine as _;
use base64::engine::general_purpose::STANDARD as BASE64;
use prost_types::Timestamp;

use crate::public::generated::rpc_support::{GestaltError, gestalt_error_code};

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

pub(crate) fn decode_bool(value: &serde_json::Value) -> Result<bool, GestaltError> {
    value
        .as_bool()
        .ok_or_else(|| invalid_argument("expected bool"))
}

pub(crate) fn decode_string(value: &serde_json::Value) -> Result<String, GestaltError> {
    value
        .as_str()
        .map(str::to_string)
        .ok_or_else(|| invalid_argument("expected string"))
}

pub(crate) fn decode_i32(value: &serde_json::Value) -> Result<i32, GestaltError> {
    match value {
        serde_json::Value::Number(number) => number
            .as_i64()
            .and_then(|v| i32::try_from(v).ok())
            .ok_or_else(|| invalid_argument("int32 out of range")),
        _ => Err(invalid_argument("expected int32")),
    }
}

pub(crate) fn decode_u32(value: &serde_json::Value) -> Result<u32, GestaltError> {
    match value {
        serde_json::Value::Number(number) => number
            .as_u64()
            .and_then(|v| u32::try_from(v).ok())
            .ok_or_else(|| invalid_argument("uint32 out of range")),
        _ => Err(invalid_argument("expected uint32")),
    }
}

pub(crate) fn decode_f32(value: &serde_json::Value) -> Result<f32, GestaltError> {
    let number = decode_f64(value)?;
    if !number.is_finite() {
        return Ok(number as f32);
    }
    if number < f64::from(f32::MIN) || number > f64::from(f32::MAX) {
        return Err(invalid_argument("float out of range"));
    }
    Ok(number as f32)
}

pub(crate) fn encode_f32(value: f32) -> serde_json::Value {
    encode_f64(f64::from(value))
}

pub(crate) fn encode_f64(value: f64) -> serde_json::Value {
    if value.is_nan() {
        return serde_json::Value::String("NaN".to_string());
    }
    if value.is_infinite() {
        return serde_json::Value::String(if value.is_sign_positive() {
            "Infinity".to_string()
        } else {
            "-Infinity".to_string()
        });
    }
    serde_json::json!(value)
}

pub(crate) fn decode_f64(value: &serde_json::Value) -> Result<f64, GestaltError> {
    match value {
        serde_json::Value::Number(number) => number
            .as_f64()
            .ok_or_else(|| invalid_argument("expected number")),
        serde_json::Value::String(text) => match text.as_str() {
            "NaN" => Ok(f64::NAN),
            "Infinity" => Ok(f64::INFINITY),
            "-Infinity" => Ok(f64::NEG_INFINITY),
            _ => Err(invalid_argument("expected number")),
        },
        _ => Err(invalid_argument("expected number")),
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
    serde_json::Value::String(format_duration(value))
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
    let time = timestamp_to_system_time(value);
    let datetime: chrono::DateTime<chrono::Utc> = time.into();
    datetime.to_rfc3339_opts(chrono::SecondsFormat::AutoSi, true)
}

fn timestamp_to_system_time(value: &Timestamp) -> std::time::SystemTime {
    let nanos = value.nanos.clamp(0, 999_999_999) as u32;
    if value.seconds >= 0 {
        UNIX_EPOCH
            .checked_add(Duration::new(value.seconds as u64, nanos))
            .unwrap_or(UNIX_EPOCH)
    } else {
        UNIX_EPOCH
            .checked_sub(Duration::new(value.seconds.unsigned_abs(), 0))
            .and_then(|time| time.checked_add(Duration::new(0, nanos)))
            .unwrap_or(UNIX_EPOCH)
    }
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

fn format_duration(value: &prost_types::Duration) -> String {
    let total_nanos = value.seconds as i128 * 1_000_000_000i128 + value.nanos as i128;
    if total_nanos == 0 {
        return "0s".to_string();
    }
    let negative = total_nanos < 0;
    let abs = total_nanos.unsigned_abs();
    let secs = abs / 1_000_000_000;
    let frac = abs % 1_000_000_000;
    if frac == 0 {
        return format!("{}{}s", if negative { "-" } else { "" }, secs);
    }
    let fractional = format!("{:09}", frac);
    let fractional = fractional.trim_end_matches('0');
    format!(
        "{}{}.{}s",
        if negative { "-" } else { "" },
        secs,
        fractional
    )
}

fn parse_duration(text: &str) -> Result<prost_types::Duration, GestaltError> {
    const MAX_SECONDS: i64 = 315_576_000_000;

    let (seconds, nanos) = parse_duration_components(text)
        .ok_or_else(|| invalid_argument("invalid duration format"))?;
    if !(-MAX_SECONDS..=MAX_SECONDS).contains(&seconds) {
        return Err(invalid_argument("duration seconds out of range"));
    }
    if !(-999_999_999..=999_999_999).contains(&nanos) {
        return Err(invalid_argument("duration nanos out of range"));
    }
    Ok(prost_types::Duration { seconds, nanos })
}

// parse_duration_components implements the protobuf-go protojson duration grammar.
fn parse_duration_components(text: &str) -> Option<(i64, i32)> {
    let b = text.as_bytes();
    if b.len() < 2 || b[b.len() - 1] != b's' {
        return None;
    }
    let mut b = &b[..b.len() - 1];

    let mut neg = false;
    match b.first().copied() {
        Some(b'-') => {
            neg = true;
            b = &b[1..];
        }
        Some(b'+') => b = &b[1..],
        _ => {}
    }
    if b.is_empty() {
        return None;
    }

    let intp;
    match b[0] {
        b'0' => {
            intp = &b[..0];
            b = &b[1..];
        }
        b'1'..=b'9' => {
            let mut n = 1usize;
            while n < b.len() && b[n].is_ascii_digit() {
                n += 1;
            }
            intp = &b[..n];
            b = &b[n..];
        }
        b'.' => {
            intp = &b[..0];
        }
        _ => return None,
    }

    let mut has_frac = false;
    let mut frac = [b'0'; 9];
    if !b.is_empty() {
        if b[0] != b'.' {
            return None;
        }
        b = &b[1..];
        let mut n = 0usize;
        while n < b.len() && n < 9 && b[n].is_ascii_digit() {
            frac[n] = b[n];
            n += 1;
        }
        if n < b.len() {
            return None;
        }
        for digit in &mut frac[n..] {
            *digit = b'0';
        }
        has_frac = true;
    }

    if intp.is_empty() && !has_frac {
        return None;
    }

    let mut seconds = if intp.is_empty() {
        0
    } else {
        std::str::from_utf8(intp).ok()?.parse().ok()?
    };

    let mut nanos = 0i32;
    if has_frac {
        let nanob = trim_left_zero_bytes(&frac);
        if !nanob.is_empty() {
            let parsed: i64 = std::str::from_utf8(nanob).ok()?.parse().ok()?;
            nanos = i32::try_from(parsed).ok()?;
        }
    }

    if neg {
        if seconds > 0 {
            seconds = -seconds;
        }
        if nanos > 0 {
            nanos = -nanos;
        }
    }

    Some((seconds, nanos))
}

fn trim_left_zero_bytes(bytes: &[u8]) -> &[u8] {
    let mut start = 0;
    while start < bytes.len() && bytes[start] == b'0' {
        start += 1;
    }
    &bytes[start..]
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
        serde_json::Value::Number(_) => Kind::NumberValue(decode_f64(value)?),
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

    #[test]
    fn int64_round_trip() {
        let encoded = encode_i64(42);
        let decoded = decode_i64(&encoded).expect("decode int64");
        assert_eq!(decoded, 42);
    }

    #[test]
    fn empty_struct_round_trip() {
        let value = prost_types::Struct {
            fields: std::collections::BTreeMap::new(),
        };
        let encoded = encode_struct(&value);
        assert_eq!(encoded, serde_json::json!({}));
        let decoded = decode_struct(&encoded).expect("decode struct");
        assert!(decoded.fields.is_empty());
    }

    #[test]
    fn reject_invalid_scalar_types() {
        assert!(decode_bool(&serde_json::json!("true")).is_err());
        assert!(decode_i32(&serde_json::json!("1")).is_err());
        assert!(decode_f64(&serde_json::json!("1.5")).is_err());
    }

    #[test]
    fn pre_epoch_timestamp_round_trip() {
        let value = Timestamp {
            seconds: -1,
            nanos: 500_000_000,
        };
        let encoded = encode_timestamp(&value);
        let decoded = decode_timestamp(&encoded).expect("decode timestamp");
        assert_eq!(decoded.seconds, -1);
        assert_eq!(decoded.nanos, 500_000_000);
        let reparsed = decode_timestamp(&encoded).expect("re-decode timestamp");
        assert_eq!(reparsed.seconds, value.seconds);
        assert_eq!(reparsed.nanos, value.nanos);
    }

    #[test]
    fn duration_protojson_parse() {
        struct Case {
            input: &'static str,
            ok: bool,
            seconds: i64,
            nanos: i32,
        }

        let cases = [
            Case {
                input: "+3s",
                ok: true,
                seconds: 3,
                nanos: 0,
            },
            Case {
                input: ".5s",
                ok: true,
                seconds: 0,
                nanos: 500_000_000,
            },
            Case {
                input: "-.001s",
                ok: true,
                seconds: 0,
                nanos: -1_000_000,
            },
            Case {
                input: "3.s",
                ok: true,
                seconds: 3,
                nanos: 0,
            },
            Case {
                input: ".1s",
                ok: true,
                seconds: 0,
                nanos: 100_000_000,
            },
            Case {
                input: "1.s",
                ok: true,
                seconds: 1,
                nanos: 0,
            },
            Case {
                input: "01s",
                ok: false,
                seconds: 0,
                nanos: 0,
            },
            Case {
                input: " 3s ",
                ok: false,
                seconds: 0,
                nanos: 0,
            },
            Case {
                input: "315576000001s",
                ok: false,
                seconds: 0,
                nanos: 0,
            },
            Case {
                input: "0.1000000000s",
                ok: false,
                seconds: 0,
                nanos: 0,
            },
        ];

        for case in cases {
            let result = decode_duration(&serde_json::json!(case.input));
            if case.ok {
                let decoded = result
                    .unwrap_or_else(|err| panic!("expected {:?} to parse: {err}", case.input));
                assert_eq!(decoded.seconds, case.seconds, "{:?}", case.input);
                assert_eq!(decoded.nanos, case.nanos, "{:?}", case.input);
            } else {
                assert!(result.is_err(), "expected {:?} to be rejected", case.input);
            }
        }
    }

    #[test]
    fn duration_round_trip() {
        let value = prost_types::Duration {
            seconds: -1,
            nanos: -500_000_000,
        };
        let encoded = encode_duration(&value);
        assert_eq!(encoded, serde_json::json!("-1.5s"));
        let decoded = decode_duration(&encoded).expect("decode duration");
        assert_eq!(decoded.seconds, -1);
        assert_eq!(decoded.nanos, -500_000_000);

        let subsecond = decode_duration(&serde_json::json!("-0.1s")).expect("decode subsecond");
        assert_eq!(subsecond.seconds, 0);
        assert_eq!(subsecond.nanos, -100_000_000);
        assert_eq!(encode_duration(&subsecond), serde_json::json!("-0.1s"));

        let max = decode_duration(&serde_json::json!("315576000000s")).expect("max duration");
        assert_eq!(max.seconds, 315_576_000_000);
        assert_eq!(max.nanos, 0);

        let min = decode_duration(&serde_json::json!("-315576000000s")).expect("min duration");
        assert_eq!(min.seconds, -315_576_000_000);
        assert_eq!(min.nanos, 0);

        let max_fractional = prost_types::Duration {
            seconds: 315_576_000_000,
            nanos: 100_000_000,
        };
        let encoded = encode_duration(&max_fractional);
        assert_eq!(encoded, serde_json::json!("315576000000.1s"));
        let decoded = decode_duration(&encoded).expect("decode max fractional");
        assert_eq!(decoded, max_fractional);
    }

    #[test]
    fn non_finite_float_round_trip() {
        let encoded = encode_f64(f64::NAN);
        assert_eq!(encoded, serde_json::json!("NaN"));
        assert!(decode_f64(&encoded).expect("decode nan").is_nan());
    }
}

/// Serde module for `Option<SystemTime>` fields that serializes as RFC3339
/// strings (protobuf JSON convention) instead of serde's default
/// `{secs_since_epoch, nanos_since_epoch}` object representation.
pub mod system_time {
    use std::time::SystemTime;

    pub fn serialize<S>(value: &Option<SystemTime>, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: serde::Serializer,
    {
        match value {
            Some(t) => {
                let dt: chrono::DateTime<chrono::Utc> = (*t).into();
                serializer.serialize_str(&dt.to_rfc3339_opts(chrono::SecondsFormat::AutoSi, true))
            }
            None => serializer.serialize_none(),
        }
    }

    pub fn deserialize<'de, D>(deserializer: D) -> Result<Option<SystemTime>, D::Error>
    where
        D: serde::Deserializer<'de>,
    {
        let opt: Option<String> = serde::Deserialize::deserialize(deserializer)?;
        match opt {
            Some(s) => chrono::DateTime::parse_from_rfc3339(&s)
                .map(|dt| Some(dt.with_timezone(&chrono::Utc).into()))
                .map_err(serde::de::Error::custom),
            None => Ok(None),
        }
    }
}
