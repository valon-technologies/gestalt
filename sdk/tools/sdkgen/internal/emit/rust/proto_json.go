package rust

import (
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

func encodeWireJSONFunc(messageFullName string) string {
	return "encode_wire_" + publicSnake(localName(messageFullName)) + "_json"
}

func decodeWireJSONFunc(messageFullName string) string {
	return "decode_wire_" + publicSnake(localName(messageFullName)) + "_json"
}

func (r *renderer) wireJSONEncodeRef(protoFile, messageFullName string) string {
	return r.convRef(protoFile, encodeWireJSONFunc(messageFullName))
}

func (r *renderer) wireJSONDecodeRef(protoFile, messageFullName string) string {
	return r.convRef(protoFile, decodeWireJSONFunc(messageFullName))
}

func (r *renderer) renderWireProtoJSON(m *model.Message, needEncode, needDecode bool) {
	if !needEncode && !needDecode {
		return
	}
	wire := r.idx.wireMessages[m.FullName]
	if wire == nil {
		wire = m
	}
	wireName := wireTypeName(m.FullName)
	r.features.v1 = true

	if needEncode {
		fmt.Fprintf(&r.body, "/// Encodes a wire `%s` as protobuf JSON.\n", wireName)
		fmt.Fprintf(&r.body, "pub(crate) fn %s(value: &v1::%s) -> serde_json::Value {\n", encodeWireJSONFunc(m.FullName), wireName)
		r.body.WriteString("    let mut object = serde_json::Map::new();\n")
		for _, f := range wire.Fields {
			if f.OneofIndex >= 0 {
				continue
			}
			r.renderWireJSONEncodeField(f, "value")
		}
		for _, o := range wire.Oneofs {
			r.renderWireJSONEncodeOneof(wire, o, "value")
		}
		r.body.WriteString("    serde_json::Value::Object(object)\n}\n\n")
	}

	if needDecode {
		fmt.Fprintf(&r.body, "/// Decodes protobuf JSON into a wire `%s`.\n", wireName)
		errRef := r.gestaltErrorRef()
		fmt.Fprintf(&r.body, "pub(crate) fn %s(value: &serde_json::Value) -> Result<v1::%s, %s::GestaltError> {\n", decodeWireJSONFunc(m.FullName), wireName, errRef)
		r.body.WriteString("    let Some(object) = value.as_object() else {\n")
		fmt.Fprintf(&r.body, "        return Err(%s::GestaltError::new(\n", errRef)
		fmt.Fprintf(&r.body, "            %s::gestalt_error_code::INVALID_ARGUMENT,\n", errRef)
		r.body.WriteString("            \"expected JSON object\",\n")
		r.body.WriteString("        ));\n")
		r.body.WriteString("    };\n")
		r.body.WriteString("    Ok(v1::")
		r.body.WriteString(wireName)
		r.body.WriteString(" {\n")
		for _, f := range wire.Fields {
			if f.OneofIndex >= 0 {
				continue
			}
			r.renderWireJSONDecodeField(f)
		}
		for _, o := range wire.Oneofs {
			r.renderWireJSONDecodeOneof(wire, o)
		}
		r.body.WriteString("        ..Default::default()\n")
		r.body.WriteString("    })\n}\n\n")
	}
}

func wireBytesExpr(expr string) string {
	return "&" + expr
}

// wireJSONScalarForm controls when scalar values need dereferencing before encode.
type wireJSONScalarForm int

const (
	wireJSONScalarDirect wireJSONScalarForm = iota
	wireJSONScalarBorrowed
)

func wireJSONScalarExpr(expr string, form wireJSONScalarForm) string {
	if form == wireJSONScalarBorrowed {
		return "*" + expr
	}
	return expr
}

func fieldToTypeRef(f *model.Field) *model.TypeRef {
	if f == nil {
		return nil
	}
	return fieldRef(f)
}

// wireJSONEncodeValue renders a Rust expression that encodes one protobuf value as JSON.
func (r *renderer) wireJSONEncodeValue(ref *model.TypeRef, expr string, form wireJSONScalarForm) string {
	if ref == nil {
		return "serde_json::Value::Null"
	}
	scalar := wireJSONScalarExpr(expr, form)
	switch ref.Kind {
	case model.KindScalar:
		switch ref.Scalar {
		case model.ScalarBool:
			return fmt.Sprintf("serde_json::Value::Bool(%s)", scalar)
		case model.ScalarString:
			return fmt.Sprintf("serde_json::Value::String(%s.to_string())", expr)
		case model.ScalarInt64, model.ScalarSint64, model.ScalarSfixed64:
			return fmt.Sprintf("crate::public::proto_json::encode_i64(%s)", scalar)
		case model.ScalarUint64, model.ScalarFixed64:
			return fmt.Sprintf("crate::public::proto_json::encode_u64(%s)", scalar)
		case model.ScalarFloat:
			return fmt.Sprintf("crate::public::proto_json::encode_f32(%s)", scalar)
		case model.ScalarDouble:
			return fmt.Sprintf("crate::public::proto_json::encode_f64(%s)", scalar)
		default:
			return fmt.Sprintf("serde_json::json!(%s)", scalar)
		}
	case model.KindBytes:
		if form == wireJSONScalarBorrowed {
			return fmt.Sprintf("crate::public::proto_json::encode_bytes(%s)", expr)
		}
		return fmt.Sprintf("crate::public::proto_json::encode_bytes(%s)", wireBytesExpr(expr))
	case model.KindEnum:
		val := wireJSONScalarExpr(expr, form)
		return fmt.Sprintf("{ let v = %s; if let Some(name) = %s { serde_json::Value::String(name.to_string()) } else { serde_json::json!(v) } }",
			val, r.enumWireJSONNameExpr(ref.Enum, val))
	case model.KindMessage:
		fn := encodeWireJSONFunc(ref.Message)
		if msg := r.idx.messages[ref.Message]; msg != nil {
			fn = r.wireJSONEncodeRef(msg.ProtoFile, ref.Message)
		}
		if form == wireJSONScalarBorrowed {
			return fmt.Sprintf("%s(%s)", fn, expr)
		}
		return fmt.Sprintf("%s(&%s)", fn, expr)
	case model.KindTimestamp:
		return fmt.Sprintf("crate::public::proto_json::encode_timestamp(%s)", expr)
	case model.KindDuration:
		return fmt.Sprintf("crate::public::proto_json::encode_duration(%s)", expr)
	case model.KindJSONStruct:
		return fmt.Sprintf("crate::public::proto_json::encode_struct(%s)", expr)
	case model.KindJSONValue:
		return fmt.Sprintf("crate::public::proto_json::encode_value(%s)", expr)
	default:
		return fmt.Sprintf("serde_json::json!(%s)", expr)
	}
}

func (r *renderer) wireJSONEncodeUnitOneof(f *model.Field) string {
	switch f.Kind {
	case model.KindJSONNull:
		return "serde_json::Value::Null"
	case model.KindUnit:
		return "serde_json::json!({})"
	default:
		panic(fmt.Sprintf("rust proto_json: unexpected unit oneof field kind %v", f.Kind))
	}
}

func (r *renderer) renderWireJSONEncodeField(f *model.Field, root string) {
	key := f.JSONName
	expr := root + "." + escapeIdent(f.Name)
	if f.Presence == model.ExplicitPresence {
		fmt.Fprintf(&r.body, "    if let Some(inner) = &%s {\n", expr)
		r.renderWireJSONEncodeFieldInner(f, key, "inner")
		r.body.WriteString("    }\n")
		return
	}
	r.renderWireJSONEncodeFieldInner(f, key, expr)
}

func (r *renderer) renderWireJSONEncodeFieldInner(f *model.Field, key, expr string) {
	form := wireJSONScalarDirect
	if expr == "inner" {
		form = wireJSONScalarBorrowed
	}
	ref := fieldToTypeRef(f)
	switch f.Kind {
	case model.KindScalar:
		switch f.Scalar {
		case model.ScalarBool:
			fmt.Fprintf(&r.body, "    if %s {\n", wireJSONScalarExpr(expr, form))
			fmt.Fprintf(&r.body, "        object.insert(%q.into(), %s);\n", key, r.wireJSONEncodeValue(ref, expr, form))
			r.body.WriteString("    }\n")
		case model.ScalarString:
			fmt.Fprintf(&r.body, "    if !%s.is_empty() {\n", expr)
			fmt.Fprintf(&r.body, "        object.insert(%q.into(), %s);\n", key, r.wireJSONEncodeValue(ref, expr, form))
			r.body.WriteString("    }\n")
		case model.ScalarFloat, model.ScalarDouble:
			fmt.Fprintf(&r.body, "    if %s != 0.0 {\n", wireJSONScalarExpr(expr, form))
			fmt.Fprintf(&r.body, "        object.insert(%q.into(), %s);\n", key, r.wireJSONEncodeValue(ref, expr, form))
			r.body.WriteString("    }\n")
		default:
			fmt.Fprintf(&r.body, "    if %s != 0 {\n", wireJSONScalarExpr(expr, form))
			fmt.Fprintf(&r.body, "        object.insert(%q.into(), %s);\n", key, r.wireJSONEncodeValue(ref, expr, form))
			r.body.WriteString("    }\n")
		}
	case model.KindBytes:
		fmt.Fprintf(&r.body, "    if !%s.is_empty() {\n", expr)
		fmt.Fprintf(&r.body, "        object.insert(%q.into(), %s);\n", key, r.wireJSONEncodeValue(ref, expr, form))
		r.body.WriteString("    }\n")
	case model.KindEnum:
		fmt.Fprintf(&r.body, "    if %s != 0 {\n", wireJSONScalarExpr(expr, form))
		fmt.Fprintf(&r.body, "        object.insert(%q.into(), %s);\n", key, r.wireJSONEncodeValue(ref, expr, form))
		r.body.WriteString("    }\n")
	case model.KindMessage, model.KindTimestamp, model.KindDuration, model.KindJSONStruct, model.KindJSONValue:
		fmt.Fprintf(&r.body, "        object.insert(%q.into(), %s);\n", key, r.wireJSONEncodeValue(ref, expr, form))
	case model.KindRepeated:
		fmt.Fprintf(&r.body, "    if !%s.is_empty() {\n", expr)
		fmt.Fprintf(&r.body, "        object.insert(%q.into(), serde_json::Value::Array(%s.iter().map(|item| %s).collect()));\n",
			key, expr, r.wireJSONEncodeValue(f.Elem, "item", wireJSONScalarBorrowed))
		r.body.WriteString("    }\n")
	case model.KindMap:
		if !wireJSONMapKeySupported(f.MapKey) {
			panic(fmt.Sprintf("rust proto_json: unsupported map key scalar %v for field %s", f.MapKey, f.Name))
		}
		fmt.Fprintf(&r.body, "    if !%s.is_empty() {\n", expr)
		r.body.WriteString("        let mut map = serde_json::Map::new();\n")
		fmt.Fprintf(&r.body, "        for (key, value) in &%s {\n", expr)
		fmt.Fprintf(&r.body, "            map.insert(%s, %s);\n", r.wireJSONEncodeMapKey(f.MapKey, "key"), r.wireJSONEncodeValue(f.MapValue, "value", wireJSONScalarBorrowed))
		r.body.WriteString("        }\n")
		fmt.Fprintf(&r.body, "        object.insert(%q.into(), serde_json::Value::Object(map));\n", key)
		r.body.WriteString("    }\n")
	}
}

func (r *renderer) renderWireJSONDecodeField(f *model.Field) {
	key := f.JSONName
	ident := escapeIdent(f.Name)
	lookup := fmt.Sprintf("object.get(%q)", key)
	if f.Presence == model.ExplicitPresence && f.Kind == model.KindScalar {
		r.renderWireJSONDecodeOptionalScalar(f, ident, lookup)
		return
	}
	switch f.Kind {
	case model.KindScalar:
		switch f.Scalar {
		case model.ScalarBool:
			fmt.Fprintf(&r.body, "        %s: match %s {\n", ident, lookup)
			r.body.WriteString("            Some(value) => crate::public::proto_json::decode_bool(value)?,\n")
			r.body.WriteString("            None => false,\n")
			r.body.WriteString("        },\n")
		case model.ScalarString:
			fmt.Fprintf(&r.body, "        %s: match %s {\n", ident, lookup)
			r.body.WriteString("            Some(value) => crate::public::proto_json::decode_string(value)?,\n")
			r.body.WriteString("            None => String::new(),\n")
			r.body.WriteString("        },\n")
		case model.ScalarInt64, model.ScalarSint64, model.ScalarSfixed64:
			fmt.Fprintf(&r.body, "        %s: match %s {\n", ident, lookup)
			r.body.WriteString("            Some(value) => crate::public::proto_json::decode_i64(value)?,\n")
			r.body.WriteString("            None => 0,\n")
			r.body.WriteString("        },\n")
		case model.ScalarUint64, model.ScalarFixed64:
			fmt.Fprintf(&r.body, "        %s: match %s {\n", ident, lookup)
			r.body.WriteString("            Some(value) => crate::public::proto_json::decode_u64(value)?,\n")
			r.body.WriteString("            None => 0,\n")
			r.body.WriteString("        },\n")
		case model.ScalarFloat:
			fmt.Fprintf(&r.body, "        %s: match %s {\n", ident, lookup)
			r.body.WriteString("            Some(value) => crate::public::proto_json::decode_f32(value)?,\n")
			r.body.WriteString("            None => 0.0,\n")
			r.body.WriteString("        },\n")
		case model.ScalarDouble:
			fmt.Fprintf(&r.body, "        %s: match %s {\n", ident, lookup)
			r.body.WriteString("            Some(value) => crate::public::proto_json::decode_f64(value)?,\n")
			r.body.WriteString("            None => 0.0,\n")
			r.body.WriteString("        },\n")
		case model.ScalarInt32, model.ScalarSint32, model.ScalarSfixed32:
			fmt.Fprintf(&r.body, "        %s: match %s {\n", ident, lookup)
			r.body.WriteString("            Some(value) => crate::public::proto_json::decode_i32(value)?,\n")
			r.body.WriteString("            None => 0,\n")
			r.body.WriteString("        },\n")
		case model.ScalarUint32, model.ScalarFixed32:
			fmt.Fprintf(&r.body, "        %s: match %s {\n", ident, lookup)
			r.body.WriteString("            Some(value) => crate::public::proto_json::decode_u32(value)?,\n")
			r.body.WriteString("            None => 0,\n")
			r.body.WriteString("        },\n")
		default:
			fmt.Fprintf(&r.body, "        %s: match %s {\n", ident, lookup)
			fmt.Fprintf(&r.body, "            Some(value) => crate::public::proto_json::decode_i32(value)? as %s,\n", scalarType(f.Scalar))
			r.body.WriteString("            None => 0,\n")
			r.body.WriteString("        },\n")
		}
	case model.KindBytes:
		fmt.Fprintf(&r.body, "        %s: match %s {\n", ident, lookup)
		r.body.WriteString("            Some(value) => crate::public::proto_json::decode_bytes(value)?,\n")
		r.body.WriteString("            None => Vec::new(),\n")
		r.body.WriteString("        },\n")
	case model.KindEnum:
		fmt.Fprintf(&r.body, "        %s: %s.map(|value| %s).transpose()?.unwrap_or(0),\n", ident, lookup, r.enumWireJSONNumberExpr(f.Enum, "value"))
	case model.KindMessage:
		decodeFn := decodeWireJSONFunc(f.Message)
		if msg := r.idx.messages[f.Message]; msg != nil {
			decodeFn = r.wireJSONDecodeRef(msg.ProtoFile, f.Message)
		}
		if f.Presence == model.ExplicitPresence {
			fmt.Fprintf(&r.body, "        %s: %s.map(|value| %s(value)).transpose()?,\n", ident, lookup, decodeFn)
		} else {
			fmt.Fprintf(&r.body, "        %s: %s.map(|value| %s(value)).transpose()?.unwrap_or_default(),\n", ident, lookup, decodeFn)
		}
	case model.KindRepeated:
		fmt.Fprintf(&r.body, "        %s: match %s {\n", ident, lookup)
		fmt.Fprintf(&r.body, "            Some(value) => value.as_array().ok_or_else(|| crate::public::proto_json::invalid_proto_json(\"expected array for %s\"))?.iter().map(|item| %s).collect::<Result<Vec<_>, _>>()?,\n", key, r.wireJSONDecodeElemResult(f.Elem, "item"))
		r.body.WriteString("            None => Vec::new(),\n")
		r.body.WriteString("        },\n")
	case model.KindMap:
		if !wireJSONMapKeySupported(f.MapKey) {
			panic(fmt.Sprintf("rust proto_json: unsupported map key scalar %v for field %s", f.MapKey, f.Name))
		}
		fmt.Fprintf(&r.body, "        %s: match %s {\n", ident, lookup)
		r.body.WriteString("            Some(value) => {\n")
		r.body.WriteString("                let mut out = std::collections::BTreeMap::new();\n")
		r.body.WriteString("                let Some(entries) = value.as_object() else {\n")
		r.body.WriteString("                    return Err(crate::public::proto_json::invalid_proto_json(\"expected object for map\"));\n")
		r.body.WriteString("                };\n")
		r.body.WriteString("                for (key, value) in entries {\n")
		fmt.Fprintf(&r.body, "                    out.insert(%s?, %s?);\n", r.wireJSONDecodeMapKeyResult(f.MapKey, "key"), r.wireJSONDecodeMapValue(f.MapValue, "value"))
		r.body.WriteString("                }\n")
		r.body.WriteString("                out\n")
		r.body.WriteString("            }\n")
		r.body.WriteString("            None => std::collections::BTreeMap::new(),\n")
		r.body.WriteString("        },\n")
	case model.KindTimestamp:
		fmt.Fprintf(&r.body, "        %s: %s.map(|value| crate::public::proto_json::decode_timestamp(value)).transpose()?,\n", ident, lookup)
	case model.KindDuration:
		fmt.Fprintf(&r.body, "        %s: %s.map(|value| crate::public::proto_json::decode_duration(value)).transpose()?,\n", ident, lookup)
	case model.KindJSONStruct:
		fmt.Fprintf(&r.body, "        %s: %s.map(|value| crate::public::proto_json::decode_struct(value)).transpose()?,\n", ident, lookup)
	case model.KindJSONValue:
		fmt.Fprintf(&r.body, "        %s: %s.map(|value| crate::public::proto_json::decode_value(value)).transpose()?,\n", ident, lookup)
	}
}

func (r *renderer) renderWireJSONDecodeOptionalScalar(f *model.Field, ident, lookup string) {
	switch f.Scalar {
	case model.ScalarBool:
		fmt.Fprintf(&r.body, "        %s: %s.map(|value| crate::public::proto_json::decode_bool(value)).transpose()?,\n", ident, lookup)
	case model.ScalarString:
		fmt.Fprintf(&r.body, "        %s: %s.map(|value| crate::public::proto_json::decode_string(value)).transpose()?,\n", ident, lookup)
	case model.ScalarInt64, model.ScalarSint64, model.ScalarSfixed64:
		fmt.Fprintf(&r.body, "        %s: %s.map(|value| crate::public::proto_json::decode_i64(value)).transpose()?,\n", ident, lookup)
	case model.ScalarUint64, model.ScalarFixed64:
		fmt.Fprintf(&r.body, "        %s: %s.map(|value| crate::public::proto_json::decode_u64(value)).transpose()?,\n", ident, lookup)
	case model.ScalarUint32, model.ScalarFixed32:
		fmt.Fprintf(&r.body, "        %s: %s.map(|value| crate::public::proto_json::decode_u32(value)).transpose()?,\n", ident, lookup)
	case model.ScalarFloat:
		fmt.Fprintf(&r.body, "        %s: %s.map(|value| crate::public::proto_json::decode_f32(value)).transpose()?,\n", ident, lookup)
	case model.ScalarDouble:
		fmt.Fprintf(&r.body, "        %s: %s.map(|value| crate::public::proto_json::decode_f64(value)).transpose()?,\n", ident, lookup)
	default:
		fmt.Fprintf(&r.body, "        %s: %s.map(|value| crate::public::proto_json::decode_i32(value)).transpose()?,\n", ident, lookup)
	}
}

func (r *renderer) renderWireJSONEncodeOneof(message *model.Message, o *model.Oneof, root string) {
	ident := escapeIdent(o.Name)
	expr := root + "." + ident
	wireKind := wireOneofKind(message, o)
	fmt.Fprintf(&r.body, "    if let Some(active) = &%s {\n", expr)
	r.body.WriteString("        match active {\n")
	for _, f := range oneofFields(message, o) {
		wireVariant := upperCamel(f.Name)
		if isUnitVariant(f) {
			fmt.Fprintf(&r.body, "            %s::%s(_) => {\n", wireKind, wireVariant)
			fmt.Fprintf(&r.body, "                object.insert(%q.into(), %s);\n", f.JSONName, r.wireJSONEncodeUnitOneof(f))
			r.body.WriteString("            }\n")
			continue
		}
		fmt.Fprintf(&r.body, "            %s::%s(inner) => {\n", wireKind, wireVariant)
		fmt.Fprintf(&r.body, "                object.insert(%q.into(), %s);\n", f.JSONName, r.wireJSONEncodeValue(fieldToTypeRef(f), "inner", wireJSONScalarBorrowed))
		r.body.WriteString("            }\n")
	}
	r.body.WriteString("        }\n")
	r.body.WriteString("    }\n")
}

func (r *renderer) renderWireJSONDecodeOneof(message *model.Message, o *model.Oneof) {
	ident := escapeIdent(o.Name)
	wireKind := wireOneofKind(message, o)
	r.body.WriteString("        " + ident + ": {\n")
	r.body.WriteString("            let mut active = None;\n")
	for _, f := range oneofFields(message, o) {
		fmt.Fprintf(&r.body, "            if let Some(value) = object.get(%q) {\n", f.JSONName)
		wireVariant := upperCamel(f.Name)
		if isUnitVariant(f) {
			fmt.Fprintf(&r.body, "                active = Some(%s::%s(%s));\n", wireKind, wireVariant, unitWirePayload(f))
		} else {
			fmt.Fprintf(&r.body, "                active = Some(%s::%s(%s));\n", wireKind, wireVariant, r.wireJSONDecodeFieldInner(f, "value"))
		}
		r.body.WriteString("            }\n")
	}
	r.body.WriteString("            active\n")
	r.body.WriteString("        },\n")
}

func (r *renderer) wireJSONDecodeFieldInner(f *model.Field, valueExpr string) string {
	switch f.Kind {
	case model.KindScalar:
		switch f.Scalar {
		case model.ScalarString:
			return fmt.Sprintf("crate::public::proto_json::decode_string(%s)?", valueExpr)
		case model.ScalarInt64, model.ScalarSint64, model.ScalarSfixed64:
			return fmt.Sprintf("crate::public::proto_json::decode_i64(%s)?", valueExpr)
		case model.ScalarUint64, model.ScalarFixed64:
			return fmt.Sprintf("crate::public::proto_json::decode_u64(%s)?", valueExpr)
		case model.ScalarBool:
			return fmt.Sprintf("crate::public::proto_json::decode_bool(%s)?", valueExpr)
		case model.ScalarFloat:
			return fmt.Sprintf("crate::public::proto_json::decode_f32(%s)?", valueExpr)
		case model.ScalarDouble:
			return fmt.Sprintf("crate::public::proto_json::decode_f64(%s)?", valueExpr)
		case model.ScalarInt32, model.ScalarSint32, model.ScalarSfixed32:
			return fmt.Sprintf("crate::public::proto_json::decode_i32(%s)?", valueExpr)
		case model.ScalarUint32, model.ScalarFixed32:
			return fmt.Sprintf("crate::public::proto_json::decode_u32(%s)?", valueExpr)
		default:
			return fmt.Sprintf("crate::public::proto_json::decode_i32(%s)? as %s", valueExpr, scalarType(f.Scalar))
		}
	case model.KindBytes:
		return fmt.Sprintf("crate::public::proto_json::decode_bytes(%s)?", valueExpr)
	case model.KindEnum:
		return fmt.Sprintf("%s?", r.enumWireJSONNumberExpr(f.Enum, valueExpr))
	case model.KindMessage:
		fn := decodeWireJSONFunc(f.Message)
		if msg := r.idx.messages[f.Message]; msg != nil {
			fn = r.wireJSONDecodeRef(msg.ProtoFile, f.Message)
		}
		return fmt.Sprintf("%s(%s)?", fn, valueExpr)
	case model.KindTimestamp:
		return fmt.Sprintf("crate::public::proto_json::decode_timestamp(%s)?", valueExpr)
	case model.KindDuration:
		return fmt.Sprintf("crate::public::proto_json::decode_duration(%s)?", valueExpr)
	case model.KindJSONStruct:
		return fmt.Sprintf("crate::public::proto_json::decode_struct(%s)?", valueExpr)
	case model.KindJSONValue:
		return fmt.Sprintf("crate::public::proto_json::decode_value(%s)?", valueExpr)
	default:
		return fmt.Sprintf("Default::default()")
	}
}

func (r *renderer) ensureWireProtoJSON(fullName string) {
	if fullName == "" || r.features.wireJSONDone[fullName] {
		return
	}
	wire := r.idx.wireMessages[fullName]
	if wire == nil {
		return
	}
	r.features.wireJSONDone[fullName] = true
	if r.publicBase(wire.ProtoFile) != r.base {
		return
	}
	visitRef := func(ref *model.TypeRef) {
		if ref == nil || ref.Kind != model.KindMessage {
			return
		}
		r.ensureWireProtoJSON(ref.Message)
	}
	for _, f := range wire.Fields {
		switch f.Kind {
		case model.KindRepeated:
			visitRef(f.Elem)
		case model.KindMap:
			visitRef(f.MapValue)
		default:
			visitRef(fieldRef(f))
		}
	}
	for _, o := range wire.Oneofs {
		for _, f := range oneofFields(wire, o) {
			visitRef(fieldRef(f))
		}
	}
	r.renderWireProtoJSON(wire, true, true)
}

func (r *renderer) wireJSONDecodeElemResult(ref *model.TypeRef, varName string) string {
	errType := r.gestaltErrorRef() + "::GestaltError"
	switch ref.Kind {
	case model.KindScalar:
		switch ref.Scalar {
		case model.ScalarString:
			return fmt.Sprintf("Ok::<String, %s>(crate::public::proto_json::decode_string(%s)?)", errType, varName)
		case model.ScalarInt64, model.ScalarSint64, model.ScalarSfixed64:
			return fmt.Sprintf("crate::public::proto_json::decode_i64(%s)", varName)
		case model.ScalarUint64, model.ScalarFixed64:
			return fmt.Sprintf("crate::public::proto_json::decode_u64(%s)", varName)
		case model.ScalarBool:
			return fmt.Sprintf("crate::public::proto_json::decode_bool(%s)", varName)
		case model.ScalarFloat:
			return fmt.Sprintf("Ok::<%s, %s>(crate::public::proto_json::decode_f32(%s)?)", scalarType(ref.Scalar), errType, varName)
		case model.ScalarDouble:
			return fmt.Sprintf("Ok::<%s, %s>(crate::public::proto_json::decode_f64(%s)? as %s)", scalarType(ref.Scalar), errType, varName, scalarType(ref.Scalar))
		case model.ScalarInt32, model.ScalarSint32, model.ScalarSfixed32:
			return fmt.Sprintf("Ok::<%s, %s>(crate::public::proto_json::decode_i32(%s)?)", scalarType(ref.Scalar), errType, varName)
		case model.ScalarUint32, model.ScalarFixed32:
			return fmt.Sprintf("Ok::<%s, %s>(crate::public::proto_json::decode_u32(%s)?)", scalarType(ref.Scalar), errType, varName)
		default:
			return fmt.Sprintf("Ok::<%s, %s>(crate::public::proto_json::decode_i32(%s)? as %s)", scalarType(ref.Scalar), errType, varName, scalarType(ref.Scalar))
		}
	case model.KindBytes:
		return fmt.Sprintf("crate::public::proto_json::decode_bytes(%s)", varName)
	case model.KindEnum:
		return r.enumWireJSONNumberExpr(ref.Enum, varName)
	case model.KindMessage:
		fn := decodeWireJSONFunc(ref.Message)
		if msg := r.idx.messages[ref.Message]; msg != nil {
			fn = r.wireJSONDecodeRef(msg.ProtoFile, ref.Message)
		}
		return fn + "(" + varName + ")"
	case model.KindTimestamp:
		return fmt.Sprintf("crate::public::proto_json::decode_timestamp(%s)", varName)
	case model.KindDuration:
		return fmt.Sprintf("crate::public::proto_json::decode_duration(%s)", varName)
	case model.KindJSONStruct:
		return fmt.Sprintf("crate::public::proto_json::decode_struct(%s)", varName)
	case model.KindJSONValue:
		return fmt.Sprintf("crate::public::proto_json::decode_value(%s)", varName)
	default:
		return "Err(crate::public::proto_json::invalid_proto_json(\"unsupported repeated element\"))"
	}
}

func (r *renderer) wireJSONDecodeMapValue(ref *model.TypeRef, expr string) string {
	return r.wireJSONDecodeElemResult(ref, expr)
}

func wireJSONMapKeySupported(scalar model.ScalarType) bool {
	switch scalar {
	case model.ScalarString, model.ScalarBool,
		model.ScalarInt32, model.ScalarInt64,
		model.ScalarUint32, model.ScalarUint64,
		model.ScalarSint32, model.ScalarSint64,
		model.ScalarSfixed32, model.ScalarSfixed64,
		model.ScalarFixed32, model.ScalarFixed64:
		return true
	default:
		return false
	}
}

func (r *renderer) wireJSONEncodeMapKey(scalar model.ScalarType, keyExpr string) string {
	switch scalar {
	case model.ScalarString:
		return keyExpr + ".clone()"
	default:
		return keyExpr + ".to_string()"
	}
}

func (r *renderer) wireJSONDecodeMapKeyResult(scalar model.ScalarType, keyExpr string) string {
	errType := r.gestaltErrorRef() + "::GestaltError"
	switch scalar {
	case model.ScalarString:
		return fmt.Sprintf("Ok::<String, %s>(%s.to_string())", errType, keyExpr)
	case model.ScalarBool:
		return fmt.Sprintf(`match %s {
            "true" => Ok::<bool, %s>(true),
            "false" => Ok::<bool, %s>(false),
            _ => Err(crate::public::proto_json::invalid_proto_json("invalid bool map key")),
        }`, keyExpr, errType, errType)
	case model.ScalarInt32, model.ScalarSint32, model.ScalarSfixed32:
		return fmt.Sprintf("%s.parse::<i32>().map_err(|_| crate::public::proto_json::invalid_proto_json(\"invalid map key\"))", keyExpr)
	case model.ScalarUint32, model.ScalarFixed32:
		return fmt.Sprintf("%s.parse::<u32>().map_err(|_| crate::public::proto_json::invalid_proto_json(\"invalid map key\"))", keyExpr)
	case model.ScalarInt64, model.ScalarSint64, model.ScalarSfixed64:
		return fmt.Sprintf("%s.parse::<i64>().map_err(|_| crate::public::proto_json::invalid_proto_json(\"invalid map key\"))", keyExpr)
	case model.ScalarUint64, model.ScalarFixed64:
		return fmt.Sprintf("%s.parse::<u64>().map_err(|_| crate::public::proto_json::invalid_proto_json(\"invalid map key\"))", keyExpr)
	default:
		panic(fmt.Sprintf("rust proto_json: unsupported map key scalar %v", scalar))
	}
}

func (r *renderer) enumWireJSONNameExpr(enumFullName, expr string) string {
	e := r.idx.enums[enumFullName]
	if e == nil {
		return "None"
	}
	var b strings.Builder
	b.WriteString("match ")
	b.WriteString(expr)
	b.WriteString(" {\n")
	for _, v := range e.Values {
		fmt.Fprintf(&b, "            %d => Some(%q),\n", v.Number, v.Name)
	}
	b.WriteString("            _ => None,\n")
	b.WriteString("        }")
	return b.String()
}

func (r *renderer) enumWireJSONNumberExpr(enumFullName, expr string) string {
	e := r.idx.enums[enumFullName]
	if e == nil {
		return "Ok(0)"
	}
	errType := r.gestaltErrorRef() + "::GestaltError"
	var b strings.Builder
	fmt.Fprintf(&b, "match %s {\n", expr)
	b.WriteString("            serde_json::Value::String(text) => match text.as_str() {\n")
	for _, v := range e.Values {
		fmt.Fprintf(&b, "                %q => Ok::<i32, %s>(%d),\n", v.Name, errType, v.Number)
	}
	fmt.Fprintf(&b, "                _ => Err(crate::public::proto_json::invalid_proto_json(\"unknown enum value\")),\n")
	b.WriteString("            },\n")
	fmt.Fprintf(&b, "            serde_json::Value::Number(number) => number.as_i64().and_then(|v| i32::try_from(v).ok()).ok_or_else(|| crate::public::proto_json::invalid_proto_json(\"enum number out of range\")),\n")
	b.WriteString("            _ => Err(crate::public::proto_json::invalid_proto_json(\"expected enum value\")),\n")
	b.WriteString("        }")
	return b.String()
}
