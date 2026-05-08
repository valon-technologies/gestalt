use std::time::{Duration, SystemTime, UNIX_EPOCH};

use prost_types::{ListValue, Struct, Timestamp, Value, value::Kind};
use serde::Serialize;
use serde::ser::{
    self, Impossible, SerializeMap, SerializeSeq, SerializeStruct, SerializeStructVariant,
    SerializeTuple, SerializeTupleStruct, SerializeTupleVariant,
};

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

/// Serializes a JSON object-like value into a protobuf `Struct`.
pub fn struct_from_serializable<T: Serialize>(value: T) -> Result<Struct> {
    struct_from_json(json_from_serializable(value)?)
}

/// Serializes an optional JSON object-like value into an optional protobuf `Struct`.
pub fn optional_struct_from_serializable<T: Serialize>(value: Option<T>) -> Result<Option<Struct>> {
    value.map(struct_from_serializable).transpose()
}

/// Serializes a value into `serde_json::Value` for protocol conversion.
pub fn json_from_serializable<T: Serialize>(value: T) -> Result<serde_json::Value> {
    json_value_from_serializable(value)
        .map_err(|err| Error::bad_request(format!("serialize JSON value: {err}")))
}

pub(crate) fn json_value_from_serializable<T: Serialize>(
    value: T,
) -> std::result::Result<serde_json::Value, serde_json::Error> {
    value.serialize(JsonInputSerializer)
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

struct JsonInputSerializer;

impl ser::Serializer for JsonInputSerializer {
    type Error = serde_json::Error;
    type Ok = serde_json::Value;
    type SerializeMap = JsonObjectSerializer;
    type SerializeSeq = JsonArraySerializer;
    type SerializeStruct = JsonObjectSerializer;
    type SerializeStructVariant = JsonObjectSerializer;
    type SerializeTuple = JsonArraySerializer;
    type SerializeTupleStruct = JsonArraySerializer;
    type SerializeTupleVariant = JsonArraySerializer;

    fn serialize_bool(self, value: bool) -> std::result::Result<Self::Ok, Self::Error> {
        Ok(serde_json::Value::Bool(value))
    }

    fn serialize_i8(self, value: i8) -> std::result::Result<Self::Ok, Self::Error> {
        Ok(serde_json::Value::Number(value.into()))
    }

    fn serialize_i16(self, value: i16) -> std::result::Result<Self::Ok, Self::Error> {
        Ok(serde_json::Value::Number(value.into()))
    }

    fn serialize_i32(self, value: i32) -> std::result::Result<Self::Ok, Self::Error> {
        Ok(serde_json::Value::Number(value.into()))
    }

    fn serialize_i64(self, value: i64) -> std::result::Result<Self::Ok, Self::Error> {
        Ok(serde_json::Value::Number(value.into()))
    }

    fn serialize_u8(self, value: u8) -> std::result::Result<Self::Ok, Self::Error> {
        Ok(serde_json::Value::Number(value.into()))
    }

    fn serialize_u16(self, value: u16) -> std::result::Result<Self::Ok, Self::Error> {
        Ok(serde_json::Value::Number(value.into()))
    }

    fn serialize_u32(self, value: u32) -> std::result::Result<Self::Ok, Self::Error> {
        Ok(serde_json::Value::Number(value.into()))
    }

    fn serialize_u64(self, value: u64) -> std::result::Result<Self::Ok, Self::Error> {
        Ok(serde_json::Value::Number(value.into()))
    }

    fn serialize_f32(self, value: f32) -> std::result::Result<Self::Ok, Self::Error> {
        self.serialize_f64(f64::from(value))
    }

    fn serialize_f64(self, value: f64) -> std::result::Result<Self::Ok, Self::Error> {
        if !value.is_finite() {
            return Err(json_input_error("numbers must be finite"));
        }
        let number = serde_json::Number::from_f64(value)
            .ok_or_else(|| json_input_error("numbers must be finite"))?;
        Ok(serde_json::Value::Number(number))
    }

    fn serialize_char(self, value: char) -> std::result::Result<Self::Ok, Self::Error> {
        Ok(serde_json::Value::String(value.to_string()))
    }

    fn serialize_str(self, value: &str) -> std::result::Result<Self::Ok, Self::Error> {
        Ok(serde_json::Value::String(value.to_string()))
    }

    fn serialize_bytes(self, _value: &[u8]) -> std::result::Result<Self::Ok, Self::Error> {
        Err(json_input_error("bytes are not JSON-compatible"))
    }

    fn serialize_none(self) -> std::result::Result<Self::Ok, Self::Error> {
        Ok(serde_json::Value::Null)
    }

    fn serialize_some<T>(self, value: &T) -> std::result::Result<Self::Ok, Self::Error>
    where
        T: ?Sized + Serialize,
    {
        value.serialize(self)
    }

    fn serialize_unit(self) -> std::result::Result<Self::Ok, Self::Error> {
        Ok(serde_json::Value::Null)
    }

    fn serialize_unit_struct(
        self,
        _name: &'static str,
    ) -> std::result::Result<Self::Ok, Self::Error> {
        Ok(serde_json::Value::Null)
    }

    fn serialize_unit_variant(
        self,
        _name: &'static str,
        _variant_index: u32,
        variant: &'static str,
    ) -> std::result::Result<Self::Ok, Self::Error> {
        Ok(serde_json::Value::String(variant.to_string()))
    }

    fn serialize_newtype_struct<T>(
        self,
        _name: &'static str,
        value: &T,
    ) -> std::result::Result<Self::Ok, Self::Error>
    where
        T: ?Sized + Serialize,
    {
        value.serialize(self)
    }

    fn serialize_newtype_variant<T>(
        self,
        _name: &'static str,
        _variant_index: u32,
        variant: &'static str,
        value: &T,
    ) -> std::result::Result<Self::Ok, Self::Error>
    where
        T: ?Sized + Serialize,
    {
        let mut fields = serde_json::Map::new();
        fields.insert(variant.to_string(), value.serialize(JsonInputSerializer)?);
        Ok(serde_json::Value::Object(fields))
    }

    fn serialize_seq(
        self,
        len: Option<usize>,
    ) -> std::result::Result<Self::SerializeSeq, Self::Error> {
        Ok(JsonArraySerializer {
            values: Vec::with_capacity(len.unwrap_or(0)),
            variant: None,
        })
    }

    fn serialize_tuple(self, len: usize) -> std::result::Result<Self::SerializeTuple, Self::Error> {
        self.serialize_seq(Some(len))
    }

    fn serialize_tuple_struct(
        self,
        _name: &'static str,
        len: usize,
    ) -> std::result::Result<Self::SerializeTupleStruct, Self::Error> {
        self.serialize_seq(Some(len))
    }

    fn serialize_tuple_variant(
        self,
        _name: &'static str,
        _variant_index: u32,
        variant: &'static str,
        len: usize,
    ) -> std::result::Result<Self::SerializeTupleVariant, Self::Error> {
        Ok(JsonArraySerializer {
            values: Vec::with_capacity(len),
            variant: Some(variant),
        })
    }

    fn serialize_map(
        self,
        len: Option<usize>,
    ) -> std::result::Result<Self::SerializeMap, Self::Error> {
        Ok(JsonObjectSerializer {
            fields: serde_json::Map::with_capacity(len.unwrap_or(0)),
            next_key: None,
            variant: None,
        })
    }

    fn serialize_struct(
        self,
        _name: &'static str,
        len: usize,
    ) -> std::result::Result<Self::SerializeStruct, Self::Error> {
        self.serialize_map(Some(len))
    }

    fn serialize_struct_variant(
        self,
        _name: &'static str,
        _variant_index: u32,
        variant: &'static str,
        len: usize,
    ) -> std::result::Result<Self::SerializeStructVariant, Self::Error> {
        Ok(JsonObjectSerializer {
            fields: serde_json::Map::with_capacity(len),
            next_key: None,
            variant: Some(variant),
        })
    }
}

struct JsonArraySerializer {
    values: Vec<serde_json::Value>,
    variant: Option<&'static str>,
}

impl JsonArraySerializer {
    fn finish(self) -> std::result::Result<serde_json::Value, serde_json::Error> {
        let value = serde_json::Value::Array(self.values);
        if let Some(variant) = self.variant {
            let mut fields = serde_json::Map::new();
            fields.insert(variant.to_string(), value);
            return Ok(serde_json::Value::Object(fields));
        }
        Ok(value)
    }
}

impl SerializeSeq for JsonArraySerializer {
    type Error = serde_json::Error;
    type Ok = serde_json::Value;

    fn serialize_element<T>(&mut self, value: &T) -> std::result::Result<(), Self::Error>
    where
        T: ?Sized + Serialize,
    {
        self.values.push(value.serialize(JsonInputSerializer)?);
        Ok(())
    }

    fn end(self) -> std::result::Result<Self::Ok, Self::Error> {
        self.finish()
    }
}

impl SerializeTuple for JsonArraySerializer {
    type Error = serde_json::Error;
    type Ok = serde_json::Value;

    fn serialize_element<T>(&mut self, value: &T) -> std::result::Result<(), Self::Error>
    where
        T: ?Sized + Serialize,
    {
        SerializeSeq::serialize_element(self, value)
    }

    fn end(self) -> std::result::Result<Self::Ok, Self::Error> {
        self.finish()
    }
}

impl SerializeTupleStruct for JsonArraySerializer {
    type Error = serde_json::Error;
    type Ok = serde_json::Value;

    fn serialize_field<T>(&mut self, value: &T) -> std::result::Result<(), Self::Error>
    where
        T: ?Sized + Serialize,
    {
        SerializeSeq::serialize_element(self, value)
    }

    fn end(self) -> std::result::Result<Self::Ok, Self::Error> {
        self.finish()
    }
}

impl SerializeTupleVariant for JsonArraySerializer {
    type Error = serde_json::Error;
    type Ok = serde_json::Value;

    fn serialize_field<T>(&mut self, value: &T) -> std::result::Result<(), Self::Error>
    where
        T: ?Sized + Serialize,
    {
        SerializeSeq::serialize_element(self, value)
    }

    fn end(self) -> std::result::Result<Self::Ok, Self::Error> {
        self.finish()
    }
}

struct JsonObjectSerializer {
    fields: serde_json::Map<String, serde_json::Value>,
    next_key: Option<String>,
    variant: Option<&'static str>,
}

impl JsonObjectSerializer {
    fn finish(self) -> std::result::Result<serde_json::Value, serde_json::Error> {
        let value = serde_json::Value::Object(self.fields);
        if let Some(variant) = self.variant {
            let mut fields = serde_json::Map::new();
            fields.insert(variant.to_string(), value);
            return Ok(serde_json::Value::Object(fields));
        }
        Ok(value)
    }
}

impl SerializeMap for JsonObjectSerializer {
    type Error = serde_json::Error;
    type Ok = serde_json::Value;

    fn serialize_key<T>(&mut self, key: &T) -> std::result::Result<(), Self::Error>
    where
        T: ?Sized + Serialize,
    {
        self.next_key = Some(key.serialize(JsonKeySerializer)?);
        Ok(())
    }

    fn serialize_value<T>(&mut self, value: &T) -> std::result::Result<(), Self::Error>
    where
        T: ?Sized + Serialize,
    {
        let key = self
            .next_key
            .take()
            .ok_or_else(|| json_input_error("map value serialized before map key"))?;
        self.fields
            .insert(key, value.serialize(JsonInputSerializer)?);
        Ok(())
    }

    fn end(self) -> std::result::Result<Self::Ok, Self::Error> {
        self.finish()
    }
}

impl SerializeStruct for JsonObjectSerializer {
    type Error = serde_json::Error;
    type Ok = serde_json::Value;

    fn serialize_field<T>(
        &mut self,
        key: &'static str,
        value: &T,
    ) -> std::result::Result<(), Self::Error>
    where
        T: ?Sized + Serialize,
    {
        self.fields
            .insert(key.to_string(), value.serialize(JsonInputSerializer)?);
        Ok(())
    }

    fn end(self) -> std::result::Result<Self::Ok, Self::Error> {
        self.finish()
    }
}

impl SerializeStructVariant for JsonObjectSerializer {
    type Error = serde_json::Error;
    type Ok = serde_json::Value;

    fn serialize_field<T>(
        &mut self,
        key: &'static str,
        value: &T,
    ) -> std::result::Result<(), Self::Error>
    where
        T: ?Sized + Serialize,
    {
        SerializeStruct::serialize_field(self, key, value)
    }

    fn end(self) -> std::result::Result<Self::Ok, Self::Error> {
        self.finish()
    }
}

struct JsonKeySerializer;

impl ser::Serializer for JsonKeySerializer {
    type Error = serde_json::Error;
    type Ok = String;
    type SerializeMap = Impossible<String, serde_json::Error>;
    type SerializeSeq = Impossible<String, serde_json::Error>;
    type SerializeStruct = Impossible<String, serde_json::Error>;
    type SerializeStructVariant = Impossible<String, serde_json::Error>;
    type SerializeTuple = Impossible<String, serde_json::Error>;
    type SerializeTupleStruct = Impossible<String, serde_json::Error>;
    type SerializeTupleVariant = Impossible<String, serde_json::Error>;

    fn serialize_str(self, value: &str) -> std::result::Result<Self::Ok, Self::Error> {
        Ok(value.to_string())
    }

    fn serialize_newtype_struct<T>(
        self,
        _name: &'static str,
        value: &T,
    ) -> std::result::Result<Self::Ok, Self::Error>
    where
        T: ?Sized + Serialize,
    {
        value.serialize(self)
    }

    fn serialize_bool(self, _value: bool) -> std::result::Result<Self::Ok, Self::Error> {
        Err(json_input_error("map keys must be strings"))
    }

    fn serialize_i8(self, _value: i8) -> std::result::Result<Self::Ok, Self::Error> {
        Err(json_input_error("map keys must be strings"))
    }

    fn serialize_i16(self, _value: i16) -> std::result::Result<Self::Ok, Self::Error> {
        Err(json_input_error("map keys must be strings"))
    }

    fn serialize_i32(self, _value: i32) -> std::result::Result<Self::Ok, Self::Error> {
        Err(json_input_error("map keys must be strings"))
    }

    fn serialize_i64(self, _value: i64) -> std::result::Result<Self::Ok, Self::Error> {
        Err(json_input_error("map keys must be strings"))
    }

    fn serialize_u8(self, _value: u8) -> std::result::Result<Self::Ok, Self::Error> {
        Err(json_input_error("map keys must be strings"))
    }

    fn serialize_u16(self, _value: u16) -> std::result::Result<Self::Ok, Self::Error> {
        Err(json_input_error("map keys must be strings"))
    }

    fn serialize_u32(self, _value: u32) -> std::result::Result<Self::Ok, Self::Error> {
        Err(json_input_error("map keys must be strings"))
    }

    fn serialize_u64(self, _value: u64) -> std::result::Result<Self::Ok, Self::Error> {
        Err(json_input_error("map keys must be strings"))
    }

    fn serialize_f32(self, _value: f32) -> std::result::Result<Self::Ok, Self::Error> {
        Err(json_input_error("map keys must be strings"))
    }

    fn serialize_f64(self, _value: f64) -> std::result::Result<Self::Ok, Self::Error> {
        Err(json_input_error("map keys must be strings"))
    }

    fn serialize_char(self, _value: char) -> std::result::Result<Self::Ok, Self::Error> {
        Err(json_input_error("map keys must be strings"))
    }

    fn serialize_bytes(self, _value: &[u8]) -> std::result::Result<Self::Ok, Self::Error> {
        Err(json_input_error("map keys must be strings"))
    }

    fn serialize_none(self) -> std::result::Result<Self::Ok, Self::Error> {
        Err(json_input_error("map keys must be strings"))
    }

    fn serialize_some<T>(self, _value: &T) -> std::result::Result<Self::Ok, Self::Error>
    where
        T: ?Sized + Serialize,
    {
        Err(json_input_error("map keys must be strings"))
    }

    fn serialize_unit(self) -> std::result::Result<Self::Ok, Self::Error> {
        Err(json_input_error("map keys must be strings"))
    }

    fn serialize_unit_struct(
        self,
        _name: &'static str,
    ) -> std::result::Result<Self::Ok, Self::Error> {
        Err(json_input_error("map keys must be strings"))
    }

    fn serialize_unit_variant(
        self,
        _name: &'static str,
        _variant_index: u32,
        _variant: &'static str,
    ) -> std::result::Result<Self::Ok, Self::Error> {
        Err(json_input_error("map keys must be strings"))
    }

    fn serialize_newtype_variant<T>(
        self,
        _name: &'static str,
        _variant_index: u32,
        _variant: &'static str,
        _value: &T,
    ) -> std::result::Result<Self::Ok, Self::Error>
    where
        T: ?Sized + Serialize,
    {
        Err(json_input_error("map keys must be strings"))
    }

    fn serialize_seq(
        self,
        _len: Option<usize>,
    ) -> std::result::Result<Self::SerializeSeq, Self::Error> {
        Err(json_input_error("map keys must be strings"))
    }

    fn serialize_tuple(
        self,
        _len: usize,
    ) -> std::result::Result<Self::SerializeTuple, Self::Error> {
        Err(json_input_error("map keys must be strings"))
    }

    fn serialize_tuple_struct(
        self,
        _name: &'static str,
        _len: usize,
    ) -> std::result::Result<Self::SerializeTupleStruct, Self::Error> {
        Err(json_input_error("map keys must be strings"))
    }

    fn serialize_tuple_variant(
        self,
        _name: &'static str,
        _variant_index: u32,
        _variant: &'static str,
        _len: usize,
    ) -> std::result::Result<Self::SerializeTupleVariant, Self::Error> {
        Err(json_input_error("map keys must be strings"))
    }

    fn serialize_map(
        self,
        _len: Option<usize>,
    ) -> std::result::Result<Self::SerializeMap, Self::Error> {
        Err(json_input_error("map keys must be strings"))
    }

    fn serialize_struct(
        self,
        _name: &'static str,
        _len: usize,
    ) -> std::result::Result<Self::SerializeStruct, Self::Error> {
        Err(json_input_error("map keys must be strings"))
    }

    fn serialize_struct_variant(
        self,
        _name: &'static str,
        _variant_index: u32,
        _variant: &'static str,
        _len: usize,
    ) -> std::result::Result<Self::SerializeStructVariant, Self::Error> {
        Err(json_input_error("map keys must be strings"))
    }
}

fn json_input_error(message: &'static str) -> serde_json::Error {
    ser::Error::custom(message)
}
