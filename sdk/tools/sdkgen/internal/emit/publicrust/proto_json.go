package publicrust

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
		fmt.Fprintf(&r.body, "pub(crate) fn %s(value: &serde_json::Value) -> Result<v1::%s, crate::rpc_support::GestaltError> {\n", decodeWireJSONFunc(m.FullName), wireName)
		r.body.WriteString("    let Some(object) = value.as_object() else {\n")
		r.body.WriteString("        return Err(crate::rpc_support::GestaltError::new(\n")
		r.body.WriteString("            crate::rpc_support::gestalt_error_code::INVALID_ARGUMENT,\n")
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
		r.body.WriteString("    })\n}\n\n")
	}
}

func wireExpr(expr string) string {
	if expr == "inner" {
		return "*" + expr
	}
	return expr
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
	switch f.Kind {
	case model.KindScalar:
		switch f.Scalar {
		case model.ScalarBool:
			fmt.Fprintf(&r.body, "        if %s {\n", wireExpr(expr))
			fmt.Fprintf(&r.body, "            object.insert(%q.into(), serde_json::Value::Bool(%s));\n", key, wireExpr(expr))
			r.body.WriteString("        }\n")
		case model.ScalarString:
			fmt.Fprintf(&r.body, "    if !%s.is_empty() {\n", expr)
			fmt.Fprintf(&r.body, "        object.insert(%q.into(), serde_json::Value::String(%s.clone()));\n", key, expr)
			r.body.WriteString("    }\n")
		case model.ScalarInt64, model.ScalarSint64, model.ScalarSfixed64:
			fmt.Fprintf(&r.body, "    if %s != 0 {\n", expr)
			fmt.Fprintf(&r.body, "        object.insert(%q.into(), crate::public::proto_json::encode_i64(%s));\n", key, expr)
			r.body.WriteString("    }\n")
		case model.ScalarUint64, model.ScalarFixed64:
			fmt.Fprintf(&r.body, "    if %s != 0 {\n", expr)
			fmt.Fprintf(&r.body, "        object.insert(%q.into(), crate::public::proto_json::encode_u64(%s));\n", key, expr)
			r.body.WriteString("    }\n")
		default:
			fmt.Fprintf(&r.body, "    if %s != 0 {\n", expr)
			fmt.Fprintf(&r.body, "        object.insert(%q.into(), serde_json::json!(%s));\n", key, expr)
			r.body.WriteString("    }\n")
		}
	case model.KindBytes:
		fmt.Fprintf(&r.body, "    if !%s.is_empty() {\n", expr)
		fmt.Fprintf(&r.body, "        object.insert(%q.into(), crate::public::proto_json::encode_bytes(&%s));\n", key, expr)
		r.body.WriteString("    }\n")
	case model.KindEnum:
		fmt.Fprintf(&r.body, "    if %s != 0 {\n", expr)
		fmt.Fprintf(&r.body, "        if let Some(name) = %s {\n", r.enumWireJSONNameExpr(f.Enum, expr))
		fmt.Fprintf(&r.body, "            object.insert(%q.into(), serde_json::Value::String(name.to_string()));\n", key)
		r.body.WriteString("        }\n")
		r.body.WriteString("    }\n")
	case model.KindMessage:
		encodeFn := encodeWireJSONFunc(f.Message)
		if msg := r.idx.messages[f.Message]; msg != nil {
			encodeFn = r.wireJSONEncodeRef(msg.ProtoFile, f.Message)
		}
		fmt.Fprintf(&r.body, "        object.insert(%q.into(), %s(&%s));\n", key, encodeFn, expr)
	case model.KindRepeated:
		fmt.Fprintf(&r.body, "    if !%s.is_empty() {\n", expr)
		fmt.Fprintf(&r.body, "        object.insert(%q.into(), serde_json::Value::Array(%s.iter().map(|item| %s).collect()));\n",
			key, expr, r.wireJSONEncodeElem(f.Elem, "item"))
		r.body.WriteString("    }\n")
	case model.KindMap:
		fmt.Fprintf(&r.body, "    if !%s.is_empty() {\n", expr)
		r.body.WriteString("        let mut map = serde_json::Map::new();\n")
		fmt.Fprintf(&r.body, "        for (key, value) in &%s {\n", expr)
		fmt.Fprintf(&r.body, "            map.insert(key.to_string(), %s);\n", r.wireJSONEncodeMapValue(f.MapValue, "value"))
		r.body.WriteString("        }\n")
		fmt.Fprintf(&r.body, "        object.insert(%q.into(), serde_json::Value::Object(map));\n", key)
		r.body.WriteString("    }\n")
	case model.KindTimestamp:
		fmt.Fprintf(&r.body, "        object.insert(%q.into(), crate::public::proto_json::encode_timestamp(%s));\n", key, expr)
	case model.KindDuration:
		fmt.Fprintf(&r.body, "        object.insert(%q.into(), crate::public::proto_json::encode_duration(%s));\n", key, expr)
	case model.KindJSONStruct:
		fmt.Fprintf(&r.body, "    if !%s.fields.is_empty() {\n", expr)
		fmt.Fprintf(&r.body, "        object.insert(%q.into(), crate::public::proto_json::encode_struct(%s));\n", key, expr)
		r.body.WriteString("    }\n")
	case model.KindJSONValue:
		fmt.Fprintf(&r.body, "    object.insert(%q.into(), crate::public::proto_json::encode_value(%s));\n", key, expr)
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
			fmt.Fprintf(&r.body, "        %s: %s.and_then(|value| value.as_bool()).unwrap_or(false),\n", ident, lookup)
		case model.ScalarString:
			fmt.Fprintf(&r.body, "        %s: %s.and_then(|value| value.as_str()).unwrap_or_default().to_string(),\n", ident, lookup)
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
		default:
			fmt.Fprintf(&r.body, "        %s: %s.and_then(|value| value.as_i64()).unwrap_or(0) as %s,\n", ident, lookup, scalarType(f.Scalar))
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
		fmt.Fprintf(&r.body, "        %s: match %s {\n", ident, lookup)
		r.body.WriteString("            Some(value) => {\n")
		r.body.WriteString("                let mut out = std::collections::BTreeMap::new();\n")
		r.body.WriteString("                let Some(entries) = value.as_object() else {\n")
		r.body.WriteString("                    return Err(crate::public::proto_json::invalid_proto_json(\"expected object for map\"));\n")
		r.body.WriteString("                };\n")
		r.body.WriteString("                for (key, value) in entries {\n")
		fmt.Fprintf(&r.body, "                    out.insert(key.to_string(), %s?);\n", r.wireJSONDecodeElemResult(f.MapValue, "value"))
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
		fmt.Fprintf(&r.body, "        %s: %s.and_then(|value| value.as_bool()),\n", ident, lookup)
	case model.ScalarString:
		fmt.Fprintf(&r.body, "        %s: %s.and_then(|value| value.as_str()).map(|value| value.to_string()),\n", ident, lookup)
	case model.ScalarInt64, model.ScalarSint64, model.ScalarSfixed64:
		fmt.Fprintf(&r.body, "        %s: %s.map(|value| crate::public::proto_json::decode_i64(value)).transpose()?,\n", ident, lookup)
	case model.ScalarUint64, model.ScalarFixed64:
		fmt.Fprintf(&r.body, "        %s: %s.map(|value| crate::public::proto_json::decode_u64(value)).transpose()?,\n", ident, lookup)
	case model.ScalarUint32, model.ScalarFixed32:
		fmt.Fprintf(&r.body, "        %s: %s.and_then(|value| value.as_u64()).map(|value| value as u32),\n", ident, lookup)
	default:
		fmt.Fprintf(&r.body, "        %s: %s.and_then(|value| value.as_i64()).map(|value| value as %s),\n", ident, lookup, scalarType(f.Scalar))
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
			fmt.Fprintf(&r.body, "                object.insert(%q.into(), serde_json::Value::String(\"NULL_VALUE\".to_string()));\n", f.JSONName)
			r.body.WriteString("            }\n")
			continue
		}
		fmt.Fprintf(&r.body, "            %s::%s(inner) => {\n", wireKind, wireVariant)
		fmt.Fprintf(&r.body, "                object.insert(%q.into(), %s);\n", f.JSONName, r.wireJSONValueFromField(f, "inner"))
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

func (r *renderer) wireJSONValueFromField(f *model.Field, expr string) string {
	switch f.Kind {
	case model.KindScalar:
		switch f.Scalar {
		case model.ScalarString:
			return fmt.Sprintf("serde_json::Value::String(%s.to_string())", expr)
		case model.ScalarInt64, model.ScalarSint64, model.ScalarSfixed64:
			return fmt.Sprintf("crate::public::proto_json::encode_i64(%s)", expr)
		case model.ScalarUint64, model.ScalarFixed64:
			return fmt.Sprintf("crate::public::proto_json::encode_u64(%s)", expr)
		case model.ScalarBool:
			return fmt.Sprintf("serde_json::Value::Bool(%s)", wireExpr(expr))
		default:
			return fmt.Sprintf("serde_json::json!(%s)", expr)
		}
	case model.KindBytes:
		return fmt.Sprintf("crate::public::proto_json::encode_bytes(%s)", expr)
	case model.KindEnum:
		return fmt.Sprintf("match %s { Some(name) => serde_json::Value::String(name.to_string()), None => serde_json::Value::Null }", r.enumWireJSONNameExpr(f.Enum, expr))
	case model.KindMessage:
		fn := encodeWireJSONFunc(f.Message)
		if msg := r.idx.messages[f.Message]; msg != nil {
			fn = r.wireJSONEncodeRef(msg.ProtoFile, f.Message)
		}
		return fmt.Sprintf("%s(%s)", fn, expr)
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

func (r *renderer) wireJSONDecodeFieldInner(f *model.Field, valueExpr string) string {
	switch f.Kind {
	case model.KindScalar:
		switch f.Scalar {
		case model.ScalarString:
			return fmt.Sprintf("%s.as_str().unwrap_or_default().to_string()", valueExpr)
		case model.ScalarInt64, model.ScalarSint64, model.ScalarSfixed64:
			return fmt.Sprintf("crate::public::proto_json::decode_i64(%s)?", valueExpr)
		case model.ScalarUint64, model.ScalarFixed64:
			return fmt.Sprintf("crate::public::proto_json::decode_u64(%s)?", valueExpr)
		case model.ScalarBool:
			return fmt.Sprintf("%s.as_bool().unwrap_or(false)", valueExpr)
		default:
			return fmt.Sprintf("%s.as_i64().unwrap_or(0) as %s", valueExpr, scalarType(f.Scalar))
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

func (r *renderer) wireJSONEncodeElem(ref *model.TypeRef, varName string) string {
	switch ref.Kind {
	case model.KindScalar:
		if ref.Scalar == model.ScalarString {
			return "serde_json::Value::String(" + varName + ".to_string())"
		}
		if ref.Scalar == model.ScalarInt64 || ref.Scalar == model.ScalarSint64 || ref.Scalar == model.ScalarSfixed64 {
			return "crate::public::proto_json::encode_i64(*" + varName + ")"
		}
		if ref.Scalar == model.ScalarUint64 || ref.Scalar == model.ScalarFixed64 {
			return "crate::public::proto_json::encode_u64(*" + varName + ")"
		}
		return "serde_json::json!(" + varName + ")"
	case model.KindBytes:
		return "crate::public::proto_json::encode_bytes(" + varName + ")"
	case model.KindEnum:
		return fmt.Sprintf("match %s { Some(name) => serde_json::Value::String(name.to_string()), None => serde_json::Value::Null }", r.enumWireJSONNameExpr(ref.Enum, "*"+varName))
	case model.KindMessage:
		if msg := r.idx.messages[ref.Message]; msg != nil {
			return r.wireJSONEncodeRef(msg.ProtoFile, ref.Message) + "(" + varName + ")"
		}
		return encodeWireJSONFunc(ref.Message) + "(" + varName + ")"
	case model.KindJSONValue:
		return "crate::public::proto_json::encode_value(" + varName + ")"
	case model.KindJSONStruct:
		return "crate::public::proto_json::encode_struct(" + varName + ")"
	default:
		return "serde_json::json!(" + varName + ")"
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
	switch ref.Kind {
	case model.KindScalar:
		switch ref.Scalar {
		case model.ScalarString:
			return fmt.Sprintf("Ok::<String, crate::rpc_support::GestaltError>(%s.as_str().unwrap_or_default().to_string())", varName)
		case model.ScalarInt64, model.ScalarSint64, model.ScalarSfixed64:
			return fmt.Sprintf("crate::public::proto_json::decode_i64(%s)", varName)
		case model.ScalarUint64, model.ScalarFixed64:
			return fmt.Sprintf("crate::public::proto_json::decode_u64(%s)", varName)
		case model.ScalarBool:
			return fmt.Sprintf("Ok::<bool, crate::rpc_support::GestaltError>(%s.as_bool().unwrap_or(false))", varName)
		case model.ScalarFloat, model.ScalarDouble:
			return fmt.Sprintf("Ok::<%s, crate::rpc_support::GestaltError>(%s.as_f64().unwrap_or(0.0) as %s)", scalarType(ref.Scalar), varName, scalarType(ref.Scalar))
		default:
			return fmt.Sprintf("Ok::<%s, crate::rpc_support::GestaltError>(%s.as_i64().unwrap_or(0) as %s)", scalarType(ref.Scalar), varName, scalarType(ref.Scalar))
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
	default:
		return "Err(crate::public::proto_json::invalid_proto_json(\"unsupported repeated element\"))"
	}
}

func (r *renderer) wireJSONEncodeMapValue(ref *model.TypeRef, expr string) string {
	return r.wireJSONEncodeElem(ref, expr)
}

func (r *renderer) wireJSONDecodeMapValue(ref *model.TypeRef, expr string) string {
	return r.wireJSONDecodeElemResult(ref, expr)
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
	var b strings.Builder
	b.WriteString("match ")
	b.WriteString(expr)
	b.WriteString(".as_str() {\n")
	for _, v := range e.Values {
		fmt.Fprintf(&b, "            Some(%q) => Ok::<i32, crate::rpc_support::GestaltError>(%d),\n", v.Name, v.Number)
	}
	b.WriteString("            _ => Ok::<i32, crate::rpc_support::GestaltError>(0),\n")
	b.WriteString("        }")
	return b.String()
}
