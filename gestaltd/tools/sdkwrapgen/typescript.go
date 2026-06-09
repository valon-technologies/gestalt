//nolint:gocritic // Emitters assemble target-language source strings where Sprintf keeps fragments readable.
package main

import (
	"fmt"
	"strings"
)

func renderTypeScriptProviderSDK(ir ProviderSDKIR) ([]generatedFile, error) {
	path := "sdk/typescript/src/" + strings.ToLower(ir.Config.SDKName) + ".ts"
	var b strings.Builder
	b.Write(generatedHeader(path))
	fmt.Fprintf(&b, `import { create, type DescMessage, type MessageInitShape } from "@bufbuild/protobuf";
import {
  Code,
  ConnectError,
  createClient,
  type Client,
  type ServiceImpl,
} from "@connectrpc/connect";
import { EmptySchema } from "@bufbuild/protobuf/wkt";

import * as pb from "./internal/gen/v1/%s_pb.ts";
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

type NativeMessage<
  Schema extends DescMessage,
  Overrides extends object = Record<never, never>,
> = Omit<MessageInitShape<Schema>, keyof Overrides> & Overrides;

`, ir.ProtoFileBase)
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

//nolint:staticcheck // This emitter intentionally assembles generated TypeScript source fragments.
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

func renderTSFactoryAndNamespace(b *strings.Builder, ir ProviderSDKIR) {
	sdk := ir.Config.SDKName
	serviceKind := strings.ToLower(sdk)
	shared := "shared" + sdk
	impl := sdk + "Impl"
	fmt.Fprintf(b, `export function %s(
  options: %s.Options = {},
): %s.Client {
  const host = options.target === undefined
    ? requireHostServiceTarget(%q)
    : { target: options.target, token: hostServiceRelayToken() };
  const { target, token } = host;
  if (
    %[5]s &&
    %[5]s.target === target &&
    %[5]s.token === token
  ) {
    return %[5]s.client;
  }

  %[5]s?.client.close();
  const client = new %[6]s(target, token);
  %[5]s = { target, token, client };
  return client;
}

export namespace %[1]s {
  export interface Options {
    target?: string | undefined;
  }

`, sdk, sdk, sdk, serviceKind, shared, impl)
	for _, message := range ir.Messages {
		renderTSNamespaceMessage(b, ir, message)
	}
	renderTSClientInterface(b, ir)
	b.WriteString("}\n\n")
}

//nolint:staticcheck // This emitter intentionally assembles generated TypeScript source fragments.
func renderTSNamespaceMessage(b *strings.Builder, ir ProviderSDKIR, message irMessage) {
	public := tsPublicMessageName(ir, message.ProtoName)
	if oneof := message.Oneof; oneof != nil {
		b.WriteString(fmt.Sprintf("  export type %s =\n", public))
		for _, field := range oneof.Variants {
			kind := tsOneofKind(field)
			b.WriteString(fmt.Sprintf("    | { kind: %q; %s: %s }\n", kind, kind, tsFieldType(ir, field, false)))
		}
		b.WriteString("    | { kind: \"unset\" };\n\n")
		b.WriteString(fmt.Sprintf("  export const %s = {\n", public))
		b.WriteString(fmt.Sprintf("    unset: (): %s => ({ kind: \"unset\" }),\n", public))
		for _, field := range oneof.Variants {
			kind := tsOneofKind(field)
			typ := tsFieldType(ir, field, false)
			b.WriteString(fmt.Sprintf("    %s: (%s: %s): %s => ({\n", kind, kind, typ, public))
			b.WriteString(fmt.Sprintf("      kind: %q,\n", kind))
			b.WriteString(fmt.Sprintf("      %s,\n", kind))
			b.WriteString("    }),\n")
		}
		b.WriteString("  } as const;\n\n")
		return
	}

	b.WriteString(fmt.Sprintf("  export type %s = NativeMessage<typeof pb.%sSchema", public, message.ProtoName))
	overrides := tsOverrideFields(message)
	if len(overrides) == 0 {
		b.WriteString(">;\n\n")
		return
	}
	b.WriteString(", {\n")
	for _, field := range overrides {
		b.WriteString(fmt.Sprintf("    %s?: %s | undefined;\n", field.JSONName, tsFieldType(ir, field, true)))
	}
	b.WriteString("  }>;\n\n")
}

func tsOverrideFields(message irMessage) []irField {
	fields := make([]irField, 0, len(message.Fields))
	for _, field := range message.Fields {
		if tsNeedsOverrideField(field) {
			fields = append(fields, field)
		}
	}
	return fields
}

func tsNeedsOverrideField(field irField) bool {
	switch field.Kind {
	case irKindJSON, irKindTimestamp, irKindMessage:
		return true
	default:
		return false
	}
}

//nolint:staticcheck // This emitter intentionally assembles generated TypeScript source fragments.
func renderTSClientInterface(b *strings.Builder, ir ProviderSDKIR) {
	b.WriteString("  export interface Client {\n")
	for _, method := range ir.Methods {
		name := method.LowerName
		output := tsPublicMessageName(ir, method.OutputName)
		if method.EmptyInput {
			b.WriteString(fmt.Sprintf("    %s(): Promise<%s>;\n", name, output))
			continue
		}
		input := tsPublicMessageName(ir, method.InputName)
		b.WriteString(fmt.Sprintf("    %s(request: %s): Promise<%s>;\n", name, input, output))
	}
	b.WriteString("    close(): void;\n")
	b.WriteString("  }\n")
}

//nolint:staticcheck // This emitter intentionally assembles generated TypeScript source fragments.
func renderTSClient(b *strings.Builder, ir ProviderSDKIR) {
	sdk := ir.Config.SDKName
	serviceKind := strings.ToLower(sdk)
	shared := "shared" + sdk
	impl := sdk + "Impl"
	b.WriteString(fmt.Sprintf(`
class %[4]s implements %[1]s.Client {
  private readonly transport: ReturnType<typeof createHostServiceGrpcTransport>;
  private readonly client: Client<typeof pb.%[5]s>;
  private closed = false;

  constructor(target: string, token: string) {
    this.transport = createHostServiceGrpcTransport(
      parseHostServiceTarget(%[2]q, target),
      hostServiceMetadataInterceptors(token, ""),
    );
    this.client = createClient(pb.%[5]s, this.transport);
  }

  close(): void {
    if (this.closed) {
      return;
    }
    this.closed = true;
    this.transport.close();
    if (%[3]s?.client === this) {
      %[3]s = undefined;
    }
  }

  private requireOpen(): void {
    if (this.closed) {
      throw new ConnectError("%[2]s: client is closed", Code.FailedPrecondition);
    }
  }

`, sdk, serviceKind, shared, impl, ir.ServiceName))
	for _, method := range ir.Methods {
		name := method.LowerName
		output := tsPublicMessageName(ir, method.OutputName)
		if method.EmptyInput {
			b.WriteString(fmt.Sprintf("  async %s(): Promise<%s.%s> {\n", name, sdk, output))
			b.WriteString("    this.requireOpen();\n")
			b.WriteString(fmt.Sprintf("    return fromProtoMessage(%q, await this.client.%s(create(EmptySchema)));\n", method.OutputName, name))
			b.WriteString("  }\n\n")
			continue
		}
		input := tsPublicMessageName(ir, method.InputName)
		b.WriteString(fmt.Sprintf("  async %s(request: %s.%s): Promise<%s.%s> {\n", name, sdk, input, sdk, output))
		b.WriteString("    this.requireOpen();\n")
		b.WriteString(fmt.Sprintf("    return fromProtoMessage(%q, await this.client.%s(toProtoMessage(%q, request)));\n", method.OutputName, name, method.InputName))
		b.WriteString("  }\n\n")
	}
	b.WriteString("}\n\n")
	b.WriteString(fmt.Sprintf("let %s:\n", shared))
	b.WriteString(fmt.Sprintf("  | { target: string; token: string; client: %s.Client }\n", sdk))
	b.WriteString("  | undefined;\n\n")
}

//nolint:staticcheck // This emitter intentionally assembles generated TypeScript source fragments.
func renderTSProvider(b *strings.Builder, ir ProviderSDKIR) {
	sdk := ir.Config.SDKName
	serviceKind := strings.ToLower(sdk)
	providerOptions := sdk + "ProviderOptions"
	providerClass := sdk + "Provider"
	defineProvider := "define" + sdk + "Provider"
	isProvider := "is" + sdk + "Provider"
	createProviderService := "create" + sdk + "ProviderService"

	b.WriteString(fmt.Sprintf("export interface %s extends ProviderBaseOptions {\n", providerOptions))
	for _, method := range ir.Methods {
		name := method.LowerName
		output := tsPublicMessageName(ir, method.OutputName)
		if method.EmptyInput {
			b.WriteString(fmt.Sprintf("  %s: () => MaybePromise<%s.%s>;\n", name, sdk, output))
			continue
		}
		input := tsPublicMessageName(ir, method.InputName)
		returnType := fmt.Sprintf("%s.%s", sdk, output)
		b.WriteString(fmt.Sprintf("  %s: (request: %s.%s) => MaybePromise<%s>;\n", name, sdk, input, returnType))
	}
	b.WriteString("}\n\n")

	b.WriteString(fmt.Sprintf(`export class %s extends ProviderBase {
  readonly kind = %q as const;

  private readonly handlers: %s;

  constructor(options: %s) {
    super(options);
    this.handlers = options;
  }

`, providerClass, serviceKind, providerOptions, providerOptions))
	for _, method := range ir.Methods {
		name := method.LowerName
		output := tsPublicMessageName(ir, method.OutputName)
		if method.EmptyInput {
			b.WriteString(fmt.Sprintf("  %s(): Promise<%s.%s> {\n", name, sdk, output))
			b.WriteString(fmt.Sprintf("    return Promise.resolve(this.handlers.%s());\n", name))
			b.WriteString("  }\n\n")
			continue
		}
		input := tsPublicMessageName(ir, method.InputName)
		returnType := fmt.Sprintf("%s.%s", sdk, output)
		b.WriteString(fmt.Sprintf("  %s(request: %s.%s): Promise<%s> {\n", name, sdk, input, returnType))
		b.WriteString(fmt.Sprintf("    return Promise.resolve(this.handlers.%s(request));\n", name))
		b.WriteString("  }\n\n")
	}
	b.WriteString("}\n\n")
	b.WriteString(fmt.Sprintf(`export function %s(
  options: %s,
): %s {
  return new %s(options);
}

export function %s(
  value: unknown,
): value is %s {
  return (
    value instanceof %s ||
    (typeof value === "object" &&
      value !== null &&
      "kind" in value &&
      String((value as { kind?: unknown }).kind ?? "") === %q`, defineProvider, providerOptions, providerClass, providerClass, isProvider, providerClass, providerClass, serviceKind))
	for _, method := range ir.Methods {
		b.WriteString(fmt.Sprintf(" &&\n      %q in value", method.LowerName))
	}
	b.WriteString(`)
  );
}

`)
	b.WriteString(fmt.Sprintf(`export function %s(
  provider: %s,
): Partial<ServiceImpl<typeof pb.%s>> {
  return {
`, createProviderService, providerClass, ir.ServiceName))
	for _, method := range ir.Methods {
		name := method.LowerName
		label := method.HumanLabel
		b.WriteString(fmt.Sprintf("    async %s(request) {\n", name))
		b.WriteString("      try {\n")
		if method.EmptyInput {
			b.WriteString(fmt.Sprintf("        return toProtoMessage(%q, await provider.%s());\n", method.OutputName, name))
		} else {
			b.WriteString(fmt.Sprintf("        return toProtoMessage(%q, await provider.%s(fromProtoMessage(%q, request)));\n", method.OutputName, name, method.InputName))
		}
		b.WriteString("      } catch (error) {\n")
		b.WriteString(fmt.Sprintf("        throw providerRuntimeError(%q, error);\n", label))
		b.WriteString("      }\n")
		b.WriteString("    },\n")
	}
	b.WriteString("  };\n}\n\n")
}

//nolint:staticcheck // This emitter intentionally assembles generated TypeScript source fragments.
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

function providerRuntimeError(label: string, error: unknown): ConnectError {
  if (error instanceof ConnectError) {
    return error;
  }
  return new ConnectError(label + ": " + errorMessage(error), Code.Unknown);
}
`)
}

func tsFieldType(ir ProviderSDKIR, field irField, allowReadonly bool) string {
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
		typ = tsPublicMessageName(ir, field.MessageName)
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

func tsPublicMessageName(ir ProviderSDKIR, name string) string {
	return publicMessageName(ir.Config, name)
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
