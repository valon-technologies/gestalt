import {
  create,
  type MessageInitShape,
} from "@bufbuild/protobuf";

import {
  AgentOutputSchema,
  AgentStructuredOutputSchema,
  AgentTextOutputSchema,
  AgentMessagePartSchema,
  AgentMessagePartType as ProtoAgentMessagePartType,
  AgentMessageSchema,
  AgentTurnDisplaySchema,
  AgentTurnStructuredOutputSchema,
  AgentTurnTextOutputSchema,
  type AgentMessage as ProtoAgentMessage,
  type AgentMessagePart as ProtoAgentMessagePart,
  type AgentOutput as ProtoAgentOutput,
  type AgentTurn as ProtoAgentTurn,
  type AgentTurnDisplay as ProtoAgentTurnDisplay,
} from "./internal/gen/v1/agent_pb.ts";
import {
  SubjectContextSchema,
  AgentToolRefSchema,
  type SubjectContext as ProtoSubjectContext,
  type AgentToolRef as ProtoAgentToolRef,
} from "./internal/gen/v1/app_pb.ts";
import {
  jsonFromValue,
  valueFromJson,
  type JsonInput,
} from "./protocol.ts";
import {
  optionalObjectFromStruct,
  optionalStruct,
} from "./protocol-internal.ts";
import type {
  AgentOutput,
  AgentMessage,
  AgentMessagePart,
  AgentMessagePartType,
  AgentToolRef,
  AgentTurnOutput,
  AgentTurnDisplay,
} from "./agent.ts";
import type { Subject, SubjectInput } from "./api.ts";

export function agentTurnDisplayFromProto(
  display?: ProtoAgentTurnDisplay | undefined,
): AgentTurnDisplay | undefined {
  if (display === undefined) {
    return undefined;
  }
  return {
    kind: display.kind,
    phase: display.phase,
    text: display.text,
    label: display.label,
    ref: display.ref,
    parentRef: display.parentRef,
    input: display.input === undefined ? undefined : jsonFromValue(display.input) as JsonInput,
    output: display.output === undefined ? undefined : jsonFromValue(display.output) as JsonInput,
    error: display.error === undefined ? undefined : jsonFromValue(display.error) as JsonInput,
    action: display.action,
    format: display.format,
    language: display.language,
  };
}

export function agentTurnDisplayToProto(
  display: AgentTurnDisplay | undefined,
): MessageInitShape<typeof AgentTurnDisplaySchema> | undefined {
  if (!display) {
    return undefined;
  }
  return {
    kind: display.kind ?? "",
    phase: display.phase ?? "",
    text: display.text ?? "",
    label: display.label ?? "",
    ref: display.ref ?? "",
    parentRef: display.parentRef ?? "",
    input: display.input === undefined ? undefined : valueFromJson(display.input),
    output: display.output === undefined ? undefined : valueFromJson(display.output),
    error: display.error === undefined ? undefined : valueFromJson(display.error),
    action: display.action ?? "",
    format: display.format ?? "",
    language: display.language ?? "",
  };
}

export function agentOutputFromProto(
  output?: ProtoAgentOutput | undefined,
): AgentOutput | undefined {
  switch (output?.kind.case) {
    case "text":
      return { text: {} };
    case "structured":
      if (output.kind.value.schema === undefined) {
        throw new Error("output.structured.schema is required");
      }
      return {
        structured: {
          schema: optionalObjectFromStruct(output.kind.value.schema) ?? {},
        },
      };
    default:
      return undefined;
  }
}

export function agentOutputToProto(
  output: AgentOutput | undefined,
): MessageInitShape<typeof AgentOutputSchema> | undefined {
  if (output === undefined) {
    throw new Error("agent output is required");
  }
  const textSet = output.text !== undefined;
  const structuredSet = output.structured !== undefined;
  if (textSet === structuredSet) {
    throw new Error("exactly one of output.text or output.structured is required");
  }
  if (textSet) {
    return {
      kind: {
        case: "text",
        value: create(AgentTextOutputSchema, {}),
      },
    };
  }
  if (structuredSet) {
    if (output.structured.schema === undefined) {
      throw new Error("output.structured.schema is required");
    }
    return {
      kind: {
        case: "structured",
        value: create(AgentStructuredOutputSchema, {
          schema: optionalStruct(output.structured.schema),
        }),
      },
    };
  }
  throw new Error("exactly one of output.text or output.structured is required");
}

export function agentTurnOutputFromProto(
  output: ProtoAgentTurn["output"],
): AgentTurnOutput | undefined {
  switch (output.case) {
    case "text":
      return { text: output.value.text };
    case "structured":
      return {
        structured: {
          text: output.value.text,
          value: optionalObjectFromStruct(output.value.value),
        },
      };
    default:
      return undefined;
  }
}

export function agentTurnOutputToProto(
  output: AgentTurnOutput | undefined,
): ProtoAgentTurn["output"] {
  if (output === undefined) {
    return { case: undefined };
  }
  const textSet = output.text !== undefined;
  const structuredSet = output.structured !== undefined;
  if (textSet === structuredSet) {
    throw new Error("exactly one of output.text or output.structured is required");
  }
  if (textSet) {
    return {
      case: "text",
      value: create(AgentTurnTextOutputSchema, {
        text: output.text ?? "",
      }),
    };
  }
  if (structuredSet) {
    return {
      case: "structured",
      value: create(AgentTurnStructuredOutputSchema, {
        text: output.structured.text ?? "",
        value: optionalStruct(output.structured.value),
      }),
    };
  }
  throw new Error("exactly one of output.text or output.structured is required");
}

export function agentMessageFromProto(message: ProtoAgentMessage): AgentMessage {
  return {
    role: message.role,
    text: message.text,
    parts: message.parts.map(agentMessagePartFromProto),
    metadata: optionalObjectFromStruct(message.metadata),
  };
}

export function agentMessageToProto(
  message: AgentMessage,
): MessageInitShape<typeof AgentMessageSchema> {
  return {
    role: message.role ?? "",
    text: message.text ?? "",
    parts: message.parts?.map(agentMessagePartToProto) ?? [],
    metadata: optionalStruct(message.metadata),
  };
}

export function agentMessagePartFromProto(
  part: ProtoAgentMessagePart,
): AgentMessagePart {
  return {
    type: part.type as AgentMessagePartType,
    text: part.text,
    json: optionalObjectFromStruct(part.json),
    toolCall: part.toolCall === undefined ? undefined : {
      id: part.toolCall.id,
      toolId: part.toolCall.toolId,
      arguments: optionalObjectFromStruct(part.toolCall.arguments),
    },
    toolResult: part.toolResult === undefined ? undefined : {
      toolCallId: part.toolResult.toolCallId,
      status: part.toolResult.status,
      content: part.toolResult.content,
      output: optionalObjectFromStruct(part.toolResult.output),
    },
    imageRef: part.imageRef === undefined ? undefined : {
      uri: part.imageRef.uri,
      mimeType: part.imageRef.mimeType,
    },
  };
}

export function agentMessagePartToProto(
  part: AgentMessagePart,
): MessageInitShape<typeof AgentMessagePartSchema> {
  return {
    type: part.type ?? ProtoAgentMessagePartType.UNSPECIFIED,
    text: part.text ?? "",
    json: optionalStruct(part.json),
    toolCall: part.toolCall === undefined ? undefined : {
      id: part.toolCall.id ?? "",
      toolId: part.toolCall.toolId ?? "",
      arguments: optionalStruct(part.toolCall.arguments),
    },
    toolResult: part.toolResult === undefined ? undefined : {
      toolCallId: part.toolResult.toolCallId ?? "",
      status: part.toolResult.status ?? 0,
      content: part.toolResult.content ?? "",
      output: optionalStruct(part.toolResult.output),
    },
    imageRef: part.imageRef === undefined ? undefined : {
      uri: part.imageRef.uri ?? "",
      mimeType: part.imageRef.mimeType ?? "",
    },
  };
}

export function agentToolRefFromProto(ref: ProtoAgentToolRef): AgentToolRef {
  return {
    app: ref.app,
    operation: ref.operation,
    connection: ref.connection,
    instance: ref.instance,
    title: ref.title,
    description: ref.description,
    credentialMode: ref.credentialMode,
    system: ref.system,
    runAs: agentRunAsSubjectFromProto(ref.runAs),
  };
}

export function agentToolRefToProto(ref: AgentToolRef): ProtoAgentToolRef {
  return create(AgentToolRefSchema, {
    app: ref.app ?? "",
    operation: ref.operation ?? "",
    connection: ref.connection ?? "",
    instance: ref.instance ?? "",
    title: ref.title ?? "",
    description: ref.description ?? "",
    credentialMode: ref.credentialMode ?? "",
    system: ref.system ?? "",
    runAs: agentRunAsSubjectToProto(ref.runAs),
  });
}

function agentRunAsSubjectFromProto(
  subject?: ProtoSubjectContext | undefined,
): Subject | undefined {
  if (subject === undefined) {
    return undefined;
  }
  return {
    id: subject.id,
    credentialSubjectId: subject.credentialSubjectId,
    email: subject.email,
  };
}

function agentRunAsSubjectToProto(
  subject?: SubjectInput | undefined,
): ProtoSubjectContext | undefined {
  if (subject === undefined) {
    return undefined;
  }
  return create(SubjectContextSchema, {
    id: subject.id ?? "",
    credentialSubjectId: subject.credentialSubjectId ?? "",
    email: subject.email ?? "",
  });
}
