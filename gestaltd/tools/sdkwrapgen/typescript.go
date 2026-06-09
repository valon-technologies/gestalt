package main

import (
	"fmt"
	"strings"
)

func renderTypeScriptAuthorization(ir authorizationIR, outputs []outputConfig) ([]generatedFile, error) {
	if len(outputs) != 1 {
		return nil, fmt.Errorf("%s: typescript expects exactly one output", ir.Config.Proto)
	}
	path := outputs[0].Path

	var b strings.Builder
	b.Write(generatedHeader(path))
	b.WriteString(`import { create } from "@bufbuild/protobuf";
import {
  Code,
  ConnectError,
  createClient,
  type Client,
  type ServiceImpl,
} from "@connectrpc/connect";
import { EmptySchema } from "@bufbuild/protobuf/wkt";

import * as pb from "./internal/gen/v1/authorization_pb.ts";
import { errorMessage, type MaybePromise } from "./api.ts";
import { ProviderBase, type ProviderBaseOptions } from "./provider.ts";
import {
  dateFromTimestamp,
  jsonObjectFromStruct,
  structFromObject,
  timestampFromDate,
  type JsonObjectInput,
} from "./protocol.ts";
import {
  createHostServiceGrpcTransport,
  hostServiceRelayToken,
  hostServiceMetadataInterceptors,
  parseHostServiceTarget,
  requireHostServiceTarget,
} from "./host-service.ts";

`)
	for _, enum := range ir.Enums {
		renderTSEnum(&b, enum)
	}
	renderTSFactoryAndNamespace(&b, ir)
	renderTSClient(&b, ir)
	renderTSProvider(&b, ir)
	renderTSConversionTables(&b, ir.Messages)
	renderTSGenericConversions(&b)
	return []generatedFile{{Path: path, Data: []byte(b.String())}}, nil
}

func renderTSEnum(b *strings.Builder, enum irEnum) {
	name := enum.ProtoName
	b.WriteString(fmt.Sprintf("export const %s = {\n", name))
	for _, value := range enum.Values {
		member := value.PublicName
		b.WriteString(fmt.Sprintf("  %s: pb.%s.%s,\n", member, name, member))
	}
	b.WriteString("} as const;\n")
	b.WriteString(fmt.Sprintf("export type %s = number;\n\n", name))
}

func renderTSFactoryAndNamespace(b *strings.Builder, ir authorizationIR) {
	b.WriteString(`export function Authorization(
  options: Authorization.Options = {},
): Authorization.Client {
  const host = options.target === undefined
    ? requireHostServiceTarget("authorization")
    : { target: options.target, token: hostServiceRelayToken() };
  const { target, token } = host;
  if (
    sharedAuthorization &&
    sharedAuthorization.target === target &&
    sharedAuthorization.token === token
  ) {
    return sharedAuthorization.client;
  }

  sharedAuthorization?.client.close();
  const client = new AuthorizationImpl(target, token);
  sharedAuthorization = { target, token, client };
  return client;
}

export namespace Authorization {
  export interface Options {
    target?: string | undefined;
  }

`)
	for _, message := range ir.Messages {
		renderTSNamespaceMessage(b, message)
	}
	renderTSClientInterface(b, ir)
	b.WriteString("}\n\n")
}

func renderTSNamespaceMessage(b *strings.Builder, message irMessage) {
	public := tsPublicMessageName(message.ProtoName)
	if oneof := message.Oneof; oneof != nil {
		b.WriteString(fmt.Sprintf("  export type %s =\n", public))
		for _, field := range oneof.Variants {
			kind := tsOneofKind(field)
			b.WriteString(fmt.Sprintf("    | { kind: %q; %s: %s }\n", kind, kind, tsFieldType(field, false)))
		}
		b.WriteString("    | { kind: \"unset\" };\n\n")
		b.WriteString(fmt.Sprintf("  export const %s = {\n", public))
		b.WriteString(fmt.Sprintf("    unset: (): %s => ({ kind: \"unset\" }),\n", public))
		for _, field := range oneof.Variants {
			kind := tsOneofKind(field)
			typ := tsFieldType(field, false)
			b.WriteString(fmt.Sprintf("    %s: (%s: %s): %s => ({\n", kind, kind, typ, public))
			b.WriteString(fmt.Sprintf("      kind: %q,\n", kind))
			b.WriteString(fmt.Sprintf("      %s,\n", kind))
			b.WriteString("    }),\n")
		}
		b.WriteString("  } as const;\n\n")
		return
	}

	b.WriteString(fmt.Sprintf("  export interface %s {\n", public))
	for _, field := range message.Fields {
		b.WriteString(fmt.Sprintf("    %s?: %s | undefined;\n", field.JSONName, tsFieldType(field, true)))
	}
	b.WriteString("  }\n\n")
}

func renderTSClientInterface(b *strings.Builder, ir authorizationIR) {
	b.WriteString("  export interface Client {\n")
	for _, method := range ir.Methods {
		name := method.LowerName
		output := tsPublicMessageName(method.OutputName)
		if method.EmptyInput {
			b.WriteString(fmt.Sprintf("    %s(): Promise<%s>;\n", name, output))
			continue
		}
		input := tsPublicMessageName(method.InputName)
		b.WriteString(fmt.Sprintf("    %s(request: %s): Promise<%s>;\n", name, input, output))
	}
	b.WriteString("    close(): void;\n")
	b.WriteString("  }\n")
}

func renderTSClient(b *strings.Builder, ir authorizationIR) {
	b.WriteString(`
class AuthorizationImpl implements Authorization.Client {
  private readonly transport: ReturnType<typeof createHostServiceGrpcTransport>;
  private readonly client: Client<typeof pb.AuthorizationProvider>;
  private closed = false;

  constructor(target: string, token: string) {
    this.transport = createHostServiceGrpcTransport(
      parseHostServiceTarget("authorization", target),
      hostServiceMetadataInterceptors(token, ""),
    );
    this.client = createClient(pb.AuthorizationProvider, this.transport);
  }

  close(): void {
    if (this.closed) {
      return;
    }
    this.closed = true;
    this.transport.close();
    if (sharedAuthorization?.client === this) {
      sharedAuthorization = undefined;
    }
  }

`)
	for _, method := range ir.Methods {
		name := method.LowerName
		output := tsPublicMessageName(method.OutputName)
		if method.EmptyInput {
			b.WriteString(fmt.Sprintf("  async %s(): Promise<Authorization.%s> {\n", name, output))
			b.WriteString(fmt.Sprintf("    return fromProtoMessage(%q, await this.client.%s(create(EmptySchema)));\n", method.OutputName, name))
			b.WriteString("  }\n\n")
			continue
		}
		input := tsPublicMessageName(method.InputName)
		b.WriteString(fmt.Sprintf("  async %s(request: Authorization.%s): Promise<Authorization.%s> {\n", name, input, output))
		b.WriteString(fmt.Sprintf("    return fromProtoMessage(%q, await this.client.%s(toProtoMessage(%q, request)));\n", method.OutputName, name, method.InputName))
		b.WriteString("  }\n\n")
	}
	b.WriteString("}\n\n")
	b.WriteString("let sharedAuthorization:\n")
	b.WriteString("  | { target: string; token: string; client: Authorization.Client }\n")
	b.WriteString("  | undefined;\n\n")
}

func renderTSProvider(b *strings.Builder, ir authorizationIR) {
	b.WriteString("export interface AuthorizationProviderOptions extends ProviderBaseOptions {\n")
	for _, method := range ir.Methods {
		name := method.LowerName
		output := tsPublicMessageName(method.OutputName)
		if method.EmptyInput {
			b.WriteString(fmt.Sprintf("  %s: () => MaybePromise<Authorization.%s>;\n", name, output))
			continue
		}
		input := tsPublicMessageName(method.InputName)
		returnType := fmt.Sprintf("Authorization.%s", output)
		if method.ProtoName == "DeleteRelationship" {
			returnType += " | void"
		}
		b.WriteString(fmt.Sprintf("  %s: (request: Authorization.%s) => MaybePromise<%s>;\n", name, input, returnType))
	}
	b.WriteString("}\n\n")

	b.WriteString(`export class AuthorizationProvider extends ProviderBase {
  readonly kind = "authorization" as const;

  private readonly handlers: AuthorizationProviderOptions;

  constructor(options: AuthorizationProviderOptions) {
    super(options);
    this.handlers = options;
  }

`)
	for _, method := range ir.Methods {
		name := method.LowerName
		output := tsPublicMessageName(method.OutputName)
		if method.EmptyInput {
			b.WriteString(fmt.Sprintf("  %s(): Promise<Authorization.%s> {\n", name, output))
			b.WriteString(fmt.Sprintf("    return Promise.resolve(this.handlers.%s());\n", name))
			b.WriteString("  }\n\n")
			continue
		}
		input := tsPublicMessageName(method.InputName)
		returnType := fmt.Sprintf("Authorization.%s", output)
		if method.ProtoName == "DeleteRelationship" {
			returnType += " | void"
		}
		b.WriteString(fmt.Sprintf("  %s(request: Authorization.%s): Promise<%s> {\n", name, input, returnType))
		b.WriteString(fmt.Sprintf("    return Promise.resolve(this.handlers.%s(request));\n", name))
		b.WriteString("  }\n\n")
	}
	b.WriteString("}\n\n")
	b.WriteString(`export function defineAuthorizationProvider(
  options: AuthorizationProviderOptions,
): AuthorizationProvider {
  return new AuthorizationProvider(options);
}

export function isAuthorizationProvider(
  value: unknown,
): value is AuthorizationProvider {
  return (
    value instanceof AuthorizationProvider ||
    (typeof value === "object" &&
      value !== null &&
      "kind" in value &&
      String((value as { kind?: unknown }).kind ?? "") === "authorization"`)
	for _, method := range ir.Methods {
		b.WriteString(fmt.Sprintf(" &&\n      %q in value", method.LowerName))
	}
	b.WriteString(`)
  );
}

export function createAuthorizationProviderService(
  provider: AuthorizationProvider,
): Partial<ServiceImpl<typeof pb.AuthorizationProvider>> {
  return {
`)
	for _, method := range ir.Methods {
		name := method.LowerName
		label := method.HumanLabel
		b.WriteString(fmt.Sprintf("    async %s(request) {\n", name))
		b.WriteString("      try {\n")
		if method.EmptyInput {
			b.WriteString(fmt.Sprintf("        return toProtoMessage(%q, await provider.%s());\n", method.OutputName, name))
		} else if method.ProtoName == "DeleteRelationship" {
			b.WriteString(fmt.Sprintf("        await provider.%s(fromProtoMessage(%q, request));\n", name, method.InputName))
			b.WriteString("        return create(pb.DeleteRelationshipResponseSchema);\n")
		} else {
			b.WriteString(fmt.Sprintf("        return toProtoMessage(%q, await provider.%s(fromProtoMessage(%q, request)));\n", method.OutputName, name, method.InputName))
		}
		b.WriteString("      } catch (error) {\n")
		b.WriteString(fmt.Sprintf("        throw authorizationRuntimeError(%q, error);\n", label))
		b.WriteString("      }\n")
		b.WriteString("    },\n")
	}
	b.WriteString("  };\n}\n\n")
}

func renderTSConversionTables(b *strings.Builder, messages []irMessage) {
	b.WriteString("const messageSchemas: Record<string, unknown> = {\n")
	for _, message := range messages {
		if message.Empty {
			continue
		}
		b.WriteString(fmt.Sprintf("  %s: pb.%sSchema,\n", message.ProtoName, message.ProtoName))
	}
	b.WriteString("};\n\n")

	b.WriteString("const messageFields: Record<string, readonly FieldRule[]> = {\n")
	for _, message := range messages {
		if message.Empty || message.Oneof != nil {
			continue
		}
		b.WriteString(fmt.Sprintf("  %s: [\n", message.ProtoName))
		for _, field := range message.Fields {
			b.WriteString("    { ")
			b.WriteString(fmt.Sprintf("sdk: %q, proto: %q, kind: %q", field.JSONName, field.JSONName, tsRuleKind(field)))
			if field.Repeated {
				b.WriteString(", repeated: true")
			}
			if field.Kind == irKindMessage {
				b.WriteString(fmt.Sprintf(", message: %q", field.MessageName))
			}
			if field.Kind == irKindEnum {
				b.WriteString(fmt.Sprintf(", defaultValue: %s.%s", field.EnumName, field.DefaultEnumName))
			} else if !field.Repeated && tsRuleKind(field) != "message" && tsRuleKind(field) != "struct" && tsRuleKind(field) != "timestamp" {
				b.WriteString(fmt.Sprintf(", defaultValue: %s", tsDefaultValue(field)))
			}
			b.WriteString(" },\n")
		}
		b.WriteString("  ],\n")
	}
	b.WriteString("};\n\n")

	b.WriteString("const oneofRules: Record<string, OneofRule> = {\n")
	for _, message := range messages {
		oneof := message.Oneof
		if oneof == nil {
			continue
		}
		b.WriteString(fmt.Sprintf("  %s: {\n", message.ProtoName))
		b.WriteString(fmt.Sprintf("    proto: %q,\n", oneof.ProtoName))
		b.WriteString("    variants: [\n")
		for _, field := range oneof.Variants {
			b.WriteString("      { ")
			b.WriteString(fmt.Sprintf("kind: %q, sdk: %q, protoCase: %q, kindType: %q", tsOneofKind(field), tsOneofKind(field), field.JSONName, tsRuleKind(field)))
			if field.Kind == irKindMessage {
				b.WriteString(fmt.Sprintf(", message: %q", field.MessageName))
			}
			b.WriteString(" },\n")
		}
		b.WriteString("    ],\n")
		b.WriteString("  },\n")
	}
	b.WriteString("};\n\n")
}

func renderTSGenericConversions(b *strings.Builder) {
	b.WriteString(`interface FieldRule {
  sdk: string;
  proto: string;
  kind: "string" | "bool" | "number" | "enum" | "message" | "struct" | "timestamp";
  repeated?: boolean;
  message?: string;
  defaultValue?: unknown;
}

interface OneofVariant {
  kind: string;
  sdk: string;
  protoCase: string;
  kindType: FieldRule["kind"];
  message?: string;
}

interface OneofRule {
  proto: string;
  variants: readonly OneofVariant[];
}

function fromProtoMessage(message: string, value: unknown): any {
  if (value === undefined || value === null) {
    return undefined;
  }
  const oneof = oneofRules[message];
  if (oneof) {
    const selected = (value as any)[oneof.proto];
    const variant = oneof.variants.find((item) => item.protoCase === selected?.case);
    if (!variant) {
      return { kind: "unset" };
    }
    return {
      kind: variant.kind,
      [variant.sdk]: fromProtoValue(fieldRuleForVariant(variant), selected.value),
    };
  }

  const out: Record<string, unknown> = {};
  for (const field of messageFields[message] ?? []) {
    const raw = (value as any)[field.proto];
    if (field.repeated) {
      out[field.sdk] = Array.isArray(raw)
        ? raw.map((item) => fromProtoValue(field, item))
        : [];
      continue;
    }
    const converted = fromProtoValue(field, raw);
    if (converted !== undefined || field.kind !== "message") {
      out[field.sdk] = converted;
    }
  }
  return out;
}

function toProtoMessage(message: string, value: unknown): any {
  if (value === undefined || value === null) {
    return undefined;
  }
  const schema = messageSchemas[message];
  const oneof = oneofRules[message];
  if (oneof) {
    const selected = (value as any).kind;
    const variant = oneof.variants.find((item) => item.kind === selected);
    if (!variant) {
      return create(schema as never);
    }
    return create(schema as never, {
      [oneof.proto]: {
        case: variant.protoCase,
        value: toProtoValue(fieldRuleForVariant(variant), (value as any)[variant.sdk]),
      },
    } as never);
  }

  const out: Record<string, unknown> = {};
  for (const field of messageFields[message] ?? []) {
    const raw = (value as any)[field.sdk];
    if (field.repeated) {
      out[field.proto] = (raw ?? []).map((item: unknown) => toProtoValue(field, item));
      continue;
    }
    out[field.proto] = raw === undefined
      ? field.defaultValue
      : toProtoValue(field, raw);
  }
  return create(schema as never, out as never);
}

function fieldRuleForVariant(
  variant: OneofVariant,
): Pick<FieldRule, "kind" | "message"> {
  return variant.message === undefined
    ? { kind: variant.kindType }
    : { kind: variant.kindType, message: variant.message };
}

function fromProtoValue(field: Pick<FieldRule, "kind" | "message">, raw: unknown): unknown {
  switch (field.kind) {
    case "message":
      return fromProtoMessage(field.message ?? "", raw);
    case "struct":
      return raw === undefined ? undefined : jsonObjectFromStruct(raw as never);
    case "timestamp":
      return raw === undefined ? undefined : dateFromTimestamp(raw as never);
    default:
      return raw;
  }
}

function toProtoValue(field: Pick<FieldRule, "kind" | "message">, raw: unknown): unknown {
  switch (field.kind) {
    case "message":
      return toProtoMessage(field.message ?? "", raw);
    case "struct":
      return raw === undefined ? undefined : structFromObject(raw as JsonObjectInput);
    case "timestamp":
      return raw === undefined ? undefined : timestampFromDate(raw as Date);
    default:
      return raw;
  }
}

function authorizationRuntimeError(label: string, error: unknown): ConnectError {
  if (error instanceof ConnectError) {
    return error;
  }
  return new ConnectError(label + ": " + errorMessage(error), Code.Unknown);
}
`)
}

func tsFieldType(field irField, allowReadonly bool) string {
	var typ string
	switch field.Kind {
	case irKindString:
		typ = "string"
	case irKindBool:
		typ = "boolean"
	case irKindInt32:
		typ = "number"
	case irKindEnum:
		typ = field.EnumName
	case irKindJSON:
		typ = "JsonObjectInput"
	case irKindTimestamp:
		typ = "Date"
	case irKindMessage:
		typ = tsPublicMessageName(field.MessageName)
	default:
		typ = "unknown"
	}
	if field.Repeated {
		if allowReadonly {
			return "readonly " + typ + "[]"
		}
		return typ + "[]"
	}
	return typ
}

func tsPublicMessageName(name string) string {
	switch name {
	case "Subject", "Action", "Resource":
		return name + "Input"
	default:
		return publicMessageName(name)
	}
}

func tsRuleKind(field irField) string {
	switch field.Kind {
	case irKindString:
		return "string"
	case irKindBool:
		return "bool"
	case irKindEnum:
		return "enum"
	case irKindJSON:
		return "struct"
	case irKindTimestamp:
		return "timestamp"
	case irKindMessage:
		return "message"
	default:
		return "number"
	}
}

func tsDefaultValue(field irField) string {
	switch field.Kind {
	case irKindString:
		return `""`
	case irKindBool:
		return "false"
	default:
		return "0"
	}
}

func tsOneofKind(field irField) string {
	return lowerFirst(field.JSONName)
}

func enumValuePrefix(enumName string) string {
	var out []rune
	for i, r := range enumName {
		if i > 0 && r >= 'A' && r <= 'Z' {
			out = append(out, '_')
		}
		out = append(out, r)
	}
	return strings.ToUpper(string(out)) + "_"
}

func lowerFirst(value string) string {
	if value == "" {
		return ""
	}
	return strings.ToLower(value[:1]) + value[1:]
}

func humanMethodLabel(value string) string {
	words := []string{}
	start := 0
	for i := 1; i < len(value); i++ {
		if value[i] >= 'A' && value[i] <= 'Z' {
			words = append(words, strings.ToLower(value[start:i]))
			start = i
		}
	}
	words = append(words, strings.ToLower(value[start:]))
	return strings.Join(words, " ")
}
