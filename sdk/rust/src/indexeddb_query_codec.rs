//! Hand-written conversions from ergonomic IndexedDB queries to wire proto.

use crate::codec::indexeddb::{from_wire_key_value, to_wire_key_value};
use crate::generated::v1::{self as pb};
use crate::indexeddb::{KeyValue, KeyValueArray, KeyValueKind, TypedValue, TypedValueKind};
use crate::indexeddb_provider::Key;

/// Ergonomic query input for wire encoding.
pub(crate) enum QueryKind<'a> {
    All,
    Key(&'a Key),
    Range {
        lower: Option<&'a Key>,
        upper: Option<&'a Key>,
        lower_open: bool,
        upper_open: bool,
    },
}

/// Converts an ergonomic query to wire proto. `All` encodes as absent query (match all).
pub(crate) fn query_to_proto(query: QueryKind<'_>) -> Option<pb::IndexedDbQuery> {
    match query {
        QueryKind::All => None,
        QueryKind::Key(key) => Some(pb::IndexedDbQuery {
            query: Some(pb::indexed_db_query::Query::Key(key_to_wire_key_value(key))),
        }),
        QueryKind::Range {
            lower,
            upper,
            lower_open,
            upper_open,
        } => Some(pb::IndexedDbQuery {
            query: Some(pb::indexed_db_query::Query::Range(pb::KeyRange {
                lower: lower.map(key_to_wire_key_value),
                upper: upper.map(key_to_wire_key_value),
                lower_open,
                upper_open,
            })),
        }),
    }
}

pub(crate) fn cursor_key_to_proto(key: &Key) -> pb::KeyValue {
    key_to_wire_key_value(key)
}

pub(crate) fn key_from_wire_key_value(kv: &pb::KeyValue) -> Key {
    native_key_value_to_key(from_wire_key_value(kv.clone()))
}

pub(crate) fn key_to_wire_key_value(key: &Key) -> pb::KeyValue {
    to_wire_key_value(key_to_native_key_value(key))
}

fn key_to_native_key_value(key: &Key) -> KeyValue {
    match key {
        Key::Array(items) => KeyValue {
            kind: Some(KeyValueKind::Array(KeyValueArray {
                elements: items.iter().map(key_to_native_key_value).collect(),
            })),
        },
        scalar => KeyValue {
            kind: Some(KeyValueKind::Scalar(key_to_native_typed_value(scalar))),
        },
    }
}

fn native_key_value_to_key(value: KeyValue) -> Key {
    match value.kind {
        Some(KeyValueKind::Array(arr)) => Key::Array(
            arr.elements
                .into_iter()
                .map(native_key_value_to_key)
                .collect(),
        ),
        Some(KeyValueKind::Scalar(scalar)) => native_typed_value_to_key(scalar),
        None => panic!("indexeddb: invalid key"),
    }
}

fn key_to_native_typed_value(key: &Key) -> TypedValue {
    let kind = match key {
        Key::Int(value) => TypedValueKind::IntValue(*value),
        Key::Float(value) => TypedValueKind::FloatValue(*value),
        Key::Str(value) => TypedValueKind::StringValue(value.clone()),
        Key::Date(value) => TypedValueKind::TimeValue(*value),
        Key::Bytes(value) => TypedValueKind::BytesValue(value.clone()),
        Key::Array(_) => panic!("indexeddb: invalid key"),
    };
    TypedValue { kind: Some(kind) }
}

fn native_typed_value_to_key(value: TypedValue) -> Key {
    match value.kind {
        Some(TypedValueKind::IntValue(value)) => Key::Int(value),
        Some(TypedValueKind::FloatValue(value)) => Key::Float(value),
        Some(TypedValueKind::BoolValue(value)) => Key::Int(i64::from(value)),
        Some(TypedValueKind::StringValue(value)) => Key::Str(value),
        Some(TypedValueKind::TimeValue(value)) => Key::Date(value),
        Some(TypedValueKind::BytesValue(value)) => Key::Bytes(value),
        Some(TypedValueKind::NullValue) | Some(TypedValueKind::JsonValue(_)) | None => {
            panic!("indexeddb: invalid key")
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::{Duration, UNIX_EPOCH};

    #[test]
    fn date_key_round_trips_through_wire() {
        let key = Key::Date(UNIX_EPOCH + Duration::from_secs(1_700_000_000));
        let wire = key_to_wire_key_value(&key);
        assert_eq!(key_from_wire_key_value(&wire), key);
    }

    #[test]
    fn bytes_key_round_trips_through_wire() {
        let key = Key::Bytes(vec![0x01, 0x02, 0xff]);
        let wire = key_to_wire_key_value(&key);
        assert_eq!(key_from_wire_key_value(&wire), key);
    }
}
