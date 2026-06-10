//! Shared generated runtime for sdkgen clients: the canonical error model,
//! native representations of the well-known wire types, and the
//! conversions between them.

use std::time::{Duration, SystemTime, UNIX_EPOCH};

use crate::generated::google::rpc::Status;

/// Canonical SDK error codes, drawn from the standard gRPC status codes.
pub mod gestalt_error_code {
    /// The operation was cancelled.
    pub const CANCELLED: i32 = 1;
    /// The cause of the error is unknown.
    pub const UNKNOWN: i32 = 2;
    /// The client specified an invalid argument.
    pub const INVALID_ARGUMENT: i32 = 3;
    /// The deadline expired before the operation could complete.
    pub const DEADLINE_EXCEEDED: i32 = 4;
    /// The requested entity was not found.
    pub const NOT_FOUND: i32 = 5;
    /// The entity the client attempted to create already exists.
    pub const ALREADY_EXISTS: i32 = 6;
    /// The caller does not have permission to execute the operation.
    pub const PERMISSION_DENIED: i32 = 7;
    /// A resource has been exhausted.
    pub const RESOURCE_EXHAUSTED: i32 = 8;
    /// The system is not in a state required for the operation.
    pub const FAILED_PRECONDITION: i32 = 9;
    /// The operation was aborted.
    pub const ABORTED: i32 = 10;
    /// The operation was attempted past the valid range.
    pub const OUT_OF_RANGE: i32 = 11;
    /// The operation is not implemented or supported.
    pub const UNIMPLEMENTED: i32 = 12;
    /// An internal error occurred.
    pub const INTERNAL: i32 = 13;
    /// The service is currently unavailable.
    pub const UNAVAILABLE: i32 = 14;
    /// Unrecoverable data loss or corruption.
    pub const DATA_LOSS: i32 = 15;
    /// The request lacks valid authentication credentials.
    pub const UNAUTHENTICATED: i32 = 16;
}

/// Canonical SDK error: one numeric gRPC status code, a human-readable
/// message, and the underlying cause. Transport error types never appear in
/// public client signatures.
#[derive(Debug, thiserror::Error)]
#[error("{message}")]
pub struct GestaltError {
    /// Numeric gRPC status code, one of the gestalt_error_code constants.
    pub code: i32,
    /// Human-readable error message.
    pub message: String,
    // Boxed so the error stays small in every client Result.
    #[source]
    source: Option<Box<tonic::Status>>,
}

impl GestaltError {
    /// Creates an error with no underlying cause.
    pub fn new(code: i32, message: impl Into<String>) -> Self {
        Self {
            code,
            message: message.into(),
            source: None,
        }
    }
}

impl From<tonic::Status> for GestaltError {
    fn from(status: tonic::Status) -> Self {
        Self {
            code: status.code() as i32,
            message: status.message().to_string(),
            source: Some(Box::new(status)),
        }
    }
}

/// Native representation of google.rpc.Status carried in response payloads,
/// mirroring the canonical error model.
#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct RpcStatus {
    /// Numeric gRPC status code, one of the gestalt_error_code constants.
    pub code: i32,
    /// Human-readable error message.
    pub message: String,
}

/// Converts a native status to its wire message. Detail payloads are not part
/// of the native model and are left empty.
pub fn to_wire_status(value: RpcStatus) -> Status {
    Status {
        code: value.code,
        message: value.message,
        details: Vec::new(),
    }
}

/// Converts a wire status to its native representation, dropping any detail
/// payloads.
pub fn from_wire_status(value: Status) -> RpcStatus {
    RpcStatus {
        code: value.code,
        message: value.message,
    }
}

/// Converts a native time to its wire timestamp. Normalized wire
/// timestamps keep nanos in 0..1_000_000_000, so times before the Unix epoch
/// borrow one second when they carry nanos.
pub fn to_wire_timestamp(value: SystemTime) -> prost_types::Timestamp {
    match value.duration_since(UNIX_EPOCH) {
        Ok(elapsed) => prost_types::Timestamp {
            seconds: elapsed.as_secs() as i64,
            nanos: elapsed.subsec_nanos() as i32,
        },
        Err(err) => {
            let before = err.duration();
            let mut seconds = -(before.as_secs() as i64);
            let mut nanos = before.subsec_nanos() as i32;
            if nanos > 0 {
                seconds -= 1;
                nanos = 1_000_000_000 - nanos;
            }
            prost_types::Timestamp { seconds, nanos }
        }
    }
}

/// Converts a wire timestamp to its native time. The conversion is
/// infallible: out-of-range nanos clamp into range, and timestamps beyond
/// what SystemTime can represent clamp to the Unix epoch.
pub fn from_wire_timestamp(value: prost_types::Timestamp) -> SystemTime {
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

/// Converts a native duration to its wire duration, saturating past the wire
/// range.
pub fn to_wire_duration(value: Duration) -> prost_types::Duration {
    prost_types::Duration {
        seconds: i64::try_from(value.as_secs()).unwrap_or(i64::MAX),
        nanos: value.subsec_nanos() as i32,
    }
}

/// Converts a wire duration to its native duration. Negative wire durations
/// clamp to zero because std::time::Duration is unsigned.
pub fn from_wire_duration(value: prost_types::Duration) -> Duration {
    if value.seconds < 0 {
        return Duration::ZERO;
    }
    Duration::new(value.seconds as u64, value.nanos.clamp(0, 999_999_999) as u32)
}

/// Converts a native JSON object to its wire struct.
pub fn to_wire_struct(value: serde_json::Map<String, serde_json::Value>) -> prost_types::Struct {
    prost_types::Struct {
        fields: value
            .into_iter()
            .map(|(key, item)| (key, to_wire_value(item)))
            .collect(),
    }
}

/// Converts a wire struct to its native JSON object.
pub fn from_wire_struct(value: prost_types::Struct) -> serde_json::Map<String, serde_json::Value> {
    value
        .fields
        .into_iter()
        .map(|(key, item)| (key, from_wire_value(item)))
        .collect()
}

/// Converts a native JSON value to its wire value.
pub fn to_wire_value(value: serde_json::Value) -> prost_types::Value {
    use prost_types::value::Kind;
    let kind = match value {
        serde_json::Value::Null => Kind::NullValue(prost_types::NullValue::NullValue as i32),
        serde_json::Value::Bool(item) => Kind::BoolValue(item),
        serde_json::Value::Number(item) => Kind::NumberValue(item.as_f64().unwrap_or(0.0)),
        serde_json::Value::String(item) => Kind::StringValue(item),
        serde_json::Value::Array(items) => Kind::ListValue(prost_types::ListValue {
            values: items.into_iter().map(to_wire_value).collect(),
        }),
        serde_json::Value::Object(items) => Kind::StructValue(to_wire_struct(items)),
    };
    prost_types::Value { kind: Some(kind) }
}

/// Converts a wire value to its native JSON value. Wire numbers are f64;
/// non-finite values have no JSON representation, so they become JSON null.
pub fn from_wire_value(value: prost_types::Value) -> serde_json::Value {
    use prost_types::value::Kind;
    match value.kind {
        None | Some(Kind::NullValue(_)) => serde_json::Value::Null,
        Some(Kind::BoolValue(item)) => serde_json::Value::Bool(item),
        Some(Kind::NumberValue(item)) => serde_json::Number::from_f64(item)
            .map_or(serde_json::Value::Null, serde_json::Value::Number),
        Some(Kind::StringValue(item)) => serde_json::Value::String(item),
        Some(Kind::ListValue(items)) => {
            serde_json::Value::Array(items.values.into_iter().map(from_wire_value).collect())
        }
        Some(Kind::StructValue(items)) => serde_json::Value::Object(from_wire_struct(items)),
    }
}
