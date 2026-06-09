//nolint:gocritic // Emitters assemble target-language source strings where Sprintf keeps fragments readable.
package main

import (
	"fmt"
	"strings"
)

func renderRustAuthorization(ir authorizationIR, outputs []outputConfig) ([]generatedFile, error) {
	if len(outputs) != 1 {
		return nil, fmt.Errorf("%s: rust expects exactly one output", ir.Config.Proto)
	}
	path := outputs[0].Path
	var b strings.Builder
	b.Write(generatedHeader(path))
	b.WriteString(`#![allow(dead_code)]

use std::time::SystemTime;

use serde::Serialize;
use tonic::codegen::async_trait;

use crate::Request;
use crate::env::{ENV_HOST_SERVICE_SOCKET, ENV_HOST_SERVICE_TOKEN};
use crate::generated::v1::{
    self as pb,
    authorization_provider_client::AuthorizationProviderClient as ProtoAuthorizationProviderClient,
};
use crate::host_service::{self, HostServiceError};
use crate::protocol;

pub type JsonObject = serde_json::Map<String, serde_json::Value>;

type AuthorizationTransport = host_service::Transport;

#[derive(Debug, thiserror::Error)]
pub enum AuthorizationError {
    #[error("{0}")]
    Transport(#[from] tonic::transport::Error),
    #[error("{0}")]
    Status(#[from] tonic::Status),
    #[error("{0}")]
    Env(String),
    #[error("{0}")]
    Json(#[from] serde_json::Error),
    #[error("{0}")]
    Protocol(String),
}

impl From<HostServiceError> for AuthorizationError {
    fn from(error: HostServiceError) -> Self {
        match error {
            HostServiceError::Transport(error) => Self::Transport(error),
            HostServiceError::Env(error) => Self::Env(error),
        }
    }
}

pub fn json_object<T: Serialize>(value: T) -> Result<JsonObject, AuthorizationError> {
    match protocol::json_value_from_serializable(value)? {
        serde_json::Value::Object(fields) => Ok(fields),
        _ => Err(AuthorizationError::Protocol(
            "authorization: expected JSON object".to_string(),
        )),
    }
}

`)
	renderRustTypes(&b, ir)
	renderRustClient(&b, ir)
	renderRustConversions(&b, ir)
	return []generatedFile{{Path: path, Data: []byte(b.String())}}, nil
}

//nolint:staticcheck // This emitter intentionally assembles generated Rust source fragments.
func renderRustTypes(b *strings.Builder, ir authorizationIR) {
	for _, message := range ir.Messages {
		if message.Empty {
			continue
		}
		if message.Oneof != nil {
			b.WriteString("#[derive(Clone, Debug, PartialEq)]\n")
			b.WriteString(fmt.Sprintf("pub enum %s {\n", message.PublicName))
			for _, field := range message.Oneof.Variants {
				b.WriteString(fmt.Sprintf("    %s(%s),\n", rustOneofVariantName(field), rustType(ir, field)))
			}
			b.WriteString("    Unset,\n}\n\n")
			continue
		}
		b.WriteString("#[derive(Clone, Debug, Default, PartialEq)]\n")
		b.WriteString(fmt.Sprintf("pub struct %s {\n", message.PublicName))
		for _, field := range message.Fields {
			b.WriteString(fmt.Sprintf("    pub %s: %s,\n", field.RustName, rustFieldType(ir, field)))
		}
		b.WriteString("}\n\n")
	}
	for _, enum := range ir.Enums {
		b.WriteString("#[derive(Clone, Copy, Debug, Default, Eq, PartialEq, Hash)]\n")
		b.WriteString(fmt.Sprintf("pub enum %s {\n", enum.ProtoName))
		for i, value := range enum.Values {
			if i == 0 {
				b.WriteString("    #[default]\n")
			}
			b.WriteString(fmt.Sprintf("    %s,\n", value.RustName))
		}
		b.WriteString("    Unknown(i32),\n}\n\n")
	}
}

//nolint:staticcheck // This emitter intentionally assembles generated Rust source fragments.
func renderRustClient(b *strings.Builder, ir authorizationIR) {
	b.WriteString("#[async_trait]\npub trait AuthorizationContract: Send {\n")
	for _, method := range ir.Methods {
		output := ir.MessagesByName[method.OutputName].PublicName
		if method.EmptyInput {
			b.WriteString(fmt.Sprintf("    async fn %s(&mut self) -> Result<%s, AuthorizationError>;\n\n", method.SnakeName, output))
		} else {
			input := ir.MessagesByName[method.InputName].PublicName
			b.WriteString(fmt.Sprintf("    async fn %s(&mut self, request: %s) -> Result<%s, AuthorizationError>;\n\n", method.SnakeName, input, output))
		}
	}
	b.WriteString("}\n\n")
	b.WriteString(`pub struct Client {
    client: ProtoAuthorizationProviderClient<AuthorizationTransport>,
}

impl Client {
    pub async fn connect(_request: &Request) -> Result<Self, AuthorizationError> {
        let target = std::env::var(ENV_HOST_SERVICE_SOCKET).map_err(|_| {
            AuthorizationError::Env(format!("{ENV_HOST_SERVICE_SOCKET} is not set"))
        })?;
        let relay_token = std::env::var(ENV_HOST_SERVICE_TOKEN).unwrap_or_default();
        Self::connect_target(&target, relay_token.trim()).await
    }

    pub async fn connect_target(
        target: &str,
        relay_token: &str,
    ) -> Result<Self, AuthorizationError> {
        Ok(Self {
            client: ProtoAuthorizationProviderClient::new(
                host_service::connect("authorization", target, relay_token, None).await?,
            ),
        })
    }

`)
	for _, method := range ir.Methods {
		output := ir.MessagesByName[method.OutputName].PublicName
		if method.EmptyInput {
			b.WriteString(fmt.Sprintf("    pub async fn %s(&mut self) -> Result<%s, AuthorizationError> {\n", method.SnakeName, output))
			b.WriteString(fmt.Sprintf("        let response = self.client.%s(()).await?.into_inner();\n", method.SnakeName))
			b.WriteString(fmt.Sprintf("        %s_from_proto(response)\n    }\n\n", snakeName(output)))
			continue
		}
		input := ir.MessagesByName[method.InputName].PublicName
		b.WriteString(fmt.Sprintf("    pub async fn %s(&mut self, request: %s) -> Result<%s, AuthorizationError> {\n", method.SnakeName, input, output))
		b.WriteString(fmt.Sprintf("        let response = self.client.%s(%s_to_proto(request)).await?.into_inner();\n", method.SnakeName, snakeName(input)))
		b.WriteString(fmt.Sprintf("        %s_from_proto(response)\n    }\n\n", snakeName(output)))
	}
	b.WriteString("}\n\n")
	b.WriteString("#[async_trait]\nimpl AuthorizationContract for Client {\n")
	for _, method := range ir.Methods {
		output := ir.MessagesByName[method.OutputName].PublicName
		if method.EmptyInput {
			b.WriteString(fmt.Sprintf("    async fn %s(&mut self) -> Result<%s, AuthorizationError> {\n        Client::%s(self).await\n    }\n\n", method.SnakeName, output, method.SnakeName))
		} else {
			input := ir.MessagesByName[method.InputName].PublicName
			b.WriteString(fmt.Sprintf("    async fn %s(&mut self, request: %s) -> Result<%s, AuthorizationError> {\n        Client::%s(self, request).await\n    }\n\n", method.SnakeName, input, output, method.SnakeName))
		}
	}
	b.WriteString("}\n\n")
}

//nolint:staticcheck // This emitter intentionally assembles generated Rust source fragments.
func renderRustConversions(b *strings.Builder, ir authorizationIR) {
	for _, message := range ir.Messages {
		if message.Empty {
			continue
		}
		if message.Oneof != nil {
			renderRustOneofConversions(b, ir, message)
			continue
		}
		if len(message.Fields) == 0 {
			b.WriteString(fmt.Sprintf("fn %s_to_proto(_value: %s) -> pb::%s {\n", snakeName(message.PublicName), message.PublicName, message.ProtoRustName))
			b.WriteString(fmt.Sprintf("    pb::%s {}\n}\n\n", message.ProtoRustName))
			b.WriteString(fmt.Sprintf("fn %s_from_proto(_value: pb::%s) -> Result<%s, AuthorizationError> {\n", snakeName(message.PublicName), message.ProtoRustName, message.PublicName))
			b.WriteString(fmt.Sprintf("    Ok(%s {})\n}\n\n", message.PublicName))
			continue
		}
		b.WriteString(fmt.Sprintf("fn %s_to_proto(value: %s) -> pb::%s {\n", snakeName(message.PublicName), message.PublicName, message.ProtoRustName))
		b.WriteString(fmt.Sprintf("    pb::%s {\n", message.ProtoRustName))
		for _, field := range message.Fields {
			b.WriteString(fmt.Sprintf("        %s: %s,\n", field.RustName, rustToProtoExpr(ir, field, "value."+field.RustName)))
		}
		b.WriteString("    }\n}\n\n")
		b.WriteString(fmt.Sprintf("fn %s_from_proto(value: pb::%s) -> Result<%s, AuthorizationError> {\n", snakeName(message.PublicName), message.ProtoRustName, message.PublicName))
		b.WriteString(fmt.Sprintf("    Ok(%s {\n", message.PublicName))
		for _, field := range message.Fields {
			b.WriteString(fmt.Sprintf("        %s: %s,\n", field.RustName, rustFromProtoExpr(ir, field, "value."+field.RustName)))
		}
		b.WriteString("    })\n}\n\n")
	}
	renderRustEnumConversions(b, ir)
}

//nolint:staticcheck // This emitter intentionally assembles generated Rust source fragments.
func renderRustOneofConversions(b *strings.Builder, ir authorizationIR, message irMessage) {
	b.WriteString(fmt.Sprintf("fn %s_to_proto(value: %s) -> pb::%s {\n", snakeName(message.PublicName), message.PublicName, message.ProtoRustName))
	b.WriteString("    let kind = match value {\n")
	for _, field := range message.Oneof.Variants {
		b.WriteString(fmt.Sprintf("        %s::%s(value) => pb::%s::Kind::%s(%s),\n", message.PublicName, rustOneofVariantName(field), snakeName(message.ProtoRustName), field.ProtoGoName, rustToProtoBareExpr(ir, field, "value")))
	}
	b.WriteString(fmt.Sprintf("        %s::Unset => return pb::%s { kind: None },\n", message.PublicName, message.ProtoRustName))
	b.WriteString("    };\n")
	b.WriteString(fmt.Sprintf("    pb::%s { kind: Some(kind) }\n}\n\n", message.ProtoRustName))
	b.WriteString(fmt.Sprintf("fn %s_from_proto(value: pb::%s) -> Result<%s, AuthorizationError> {\n", snakeName(message.PublicName), message.ProtoRustName, message.PublicName))
	b.WriteString("    match value.kind {\n")
	for _, field := range message.Oneof.Variants {
		b.WriteString(fmt.Sprintf("        Some(pb::%s::Kind::%s(value)) => Ok(%s::%s(%s)),\n", snakeName(message.ProtoRustName), field.ProtoGoName, message.PublicName, rustOneofVariantName(field), rustFromProtoBareExpr(ir, field, "value")))
	}
	b.WriteString(fmt.Sprintf("        None => Ok(%s::Unset),\n", message.PublicName))
	b.WriteString("    }\n}\n\n")
}

//nolint:staticcheck // This emitter intentionally assembles generated Rust source fragments.
func renderRustEnumConversions(b *strings.Builder, ir authorizationIR) {
	for _, enum := range ir.Enums {
		lower := snakeName(enum.ProtoName)
		b.WriteString(fmt.Sprintf("fn %s_to_proto(value: %s) -> i32 {\n", lower, enum.ProtoName))
		b.WriteString("    match value {\n")
		for _, value := range enum.Values {
			b.WriteString(fmt.Sprintf("        %s::%s => pb::%s::%s as i32,\n", enum.ProtoName, value.RustName, enum.ProtoName, value.RustName))
		}
		b.WriteString(fmt.Sprintf("        %s::Unknown(value) => value,\n", enum.ProtoName))
		b.WriteString("    }\n}\n\n")
		b.WriteString(fmt.Sprintf("fn %s_from_proto(value: i32) -> %s {\n", lower, enum.ProtoName))
		b.WriteString("    match value {\n")
		for _, value := range enum.Values {
			b.WriteString(fmt.Sprintf("        value if value == pb::%s::%s as i32 => %s::%s,\n", enum.ProtoName, value.RustName, enum.ProtoName, value.RustName))
		}
		b.WriteString(fmt.Sprintf("        value => %s::Unknown(value),\n", enum.ProtoName))
		b.WriteString("    }\n}\n\n")
	}
}

func rustFieldType(ir authorizationIR, field irField) string {
	if field.Repeated {
		return "Vec<" + rustType(ir, field) + ">"
	}
	switch field.Kind {
	case irKindMessage, irKindJSON, irKindTimestamp:
		return "Option<" + rustType(ir, field) + ">"
	default:
		return rustType(ir, field)
	}
}

func rustType(ir authorizationIR, field irField) string {
	switch field.Kind {
	case irKindString:
		return "String"
	case irKindBool:
		return "bool"
	case irKindInt32:
		return "i32"
	case irKindEnum:
		return field.EnumName
	case irKindJSON:
		return "JsonObject"
	case irKindTimestamp:
		return "SystemTime"
	case irKindMessage:
		return ir.MessagesByName[field.MessageName].PublicName
	default:
		return "()"
	}
}

func rustToProtoExpr(ir authorizationIR, field irField, expr string) string {
	if field.Repeated {
		itemField := field
		itemField.Repeated = false
		switch field.Kind {
		case irKindMessage:
			return fmt.Sprintf("%s.into_iter().map(%s_to_proto).collect()", expr, snakeName(ir.MessagesByName[itemField.MessageName].PublicName))
		case irKindEnum:
			return fmt.Sprintf("%s.into_iter().map(%s_to_proto).collect()", expr, snakeName(field.EnumName))
		default:
			return expr
		}
	}
	switch field.Kind {
	case irKindMessage:
		return fmt.Sprintf("%s.map(%s_to_proto)", expr, snakeName(ir.MessagesByName[field.MessageName].PublicName))
	case irKindJSON:
		return fmt.Sprintf("%s.map(protocol::struct_from_map)", expr)
	case irKindTimestamp:
		return fmt.Sprintf("%s.map(protocol::timestamp_from_system_time)", expr)
	case irKindEnum:
		return fmt.Sprintf("%s_to_proto(%s)", snakeName(field.EnumName), expr)
	default:
		return expr
	}
}

func rustToProtoBareExpr(ir authorizationIR, field irField, expr string) string {
	switch field.Kind {
	case irKindMessage:
		return fmt.Sprintf("%s_to_proto(%s)", snakeName(ir.MessagesByName[field.MessageName].PublicName), expr)
	case irKindJSON:
		return fmt.Sprintf("protocol::struct_from_map(%s)", expr)
	case irKindTimestamp:
		return fmt.Sprintf("protocol::timestamp_from_system_time(%s)", expr)
	case irKindEnum:
		return fmt.Sprintf("%s_to_proto(%s)", snakeName(field.EnumName), expr)
	default:
		return expr
	}
}

func rustFromProtoExpr(ir authorizationIR, field irField, expr string) string {
	if field.Repeated {
		itemField := field
		itemField.Repeated = false
		switch field.Kind {
		case irKindMessage:
			return fmt.Sprintf("%s.into_iter().map(%s_from_proto).collect::<Result<Vec<_>, _>>()?", expr, snakeName(ir.MessagesByName[itemField.MessageName].PublicName))
		case irKindEnum:
			return fmt.Sprintf("%s.into_iter().map(%s_from_proto).collect()", expr, snakeName(field.EnumName))
		default:
			return expr
		}
	}
	switch field.Kind {
	case irKindMessage:
		return fmt.Sprintf("%s.map(%s_from_proto).transpose()?", expr, snakeName(ir.MessagesByName[field.MessageName].PublicName))
	case irKindJSON:
		return fmt.Sprintf("%s.map(|value| match protocol::json_from_struct(&value) { serde_json::Value::Object(fields) => Ok(fields), _ => Err(AuthorizationError::Protocol(\"authorization: expected JSON object\".to_string())) }).transpose()?", expr)
	case irKindTimestamp:
		return fmt.Sprintf("%s.map(|value| protocol::system_time_from_timestamp(&value).map_err(|error| AuthorizationError::Protocol(error.to_string()))).transpose()?", expr)
	case irKindEnum:
		return fmt.Sprintf("%s_from_proto(%s)", snakeName(field.EnumName), expr)
	default:
		return expr
	}
}

func rustFromProtoBareExpr(ir authorizationIR, field irField, expr string) string {
	switch field.Kind {
	case irKindMessage:
		return fmt.Sprintf("%s_from_proto(%s)?", snakeName(ir.MessagesByName[field.MessageName].PublicName), expr)
	case irKindJSON:
		return fmt.Sprintf("match protocol::json_from_struct(&%s) { serde_json::Value::Object(fields) => fields, _ => return Err(AuthorizationError::Protocol(\"authorization: expected JSON object\".to_string())) }", expr)
	case irKindTimestamp:
		return fmt.Sprintf("protocol::system_time_from_timestamp(&%s).map_err(|error| AuthorizationError::Protocol(error.to_string()))?", expr)
	case irKindEnum:
		return fmt.Sprintf("%s_from_proto(%s)", snakeName(field.EnumName), expr)
	default:
		return expr
	}
}

func rustOneofVariantName(field irField) string {
	return field.ProtoGoName
}
