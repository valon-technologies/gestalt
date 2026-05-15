import {
  create,
  type MessageInitShape,
} from "@bufbuild/protobuf";

import {
  AgentActorSchema,
  AgentMessagePartSchema,
  AgentMessagePartType as ProtoAgentMessagePartType,
  AgentMessageSchema,
  AgentSubjectContextSchema,
  AgentToolRefSchema,
  AgentTurnDisplaySchema,
  type AgentActor as ProtoAgentActor,
  type AgentMessage as ProtoAgentMessage,
  type AgentMessagePart as ProtoAgentMessagePart,
  type AgentSubjectContext as ProtoAgentSubjectContext,
  type AgentToolRef as ProtoAgentToolRef,
  type AgentTurnDisplay as ProtoAgentTurnDisplay,
} from "./internal/gen/v1/agent_pb.ts";
import {
  ExternalIdentityContextSchema,
  type ExternalIdentityContext as ProtoExternalIdentityContext,
} from "./internal/gen/v1/plugin_pb.ts";
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
  AgentActor,
  AgentMessage,
  AgentMessagePart,
  AgentMessagePartType,
  AgentSubjectContext,
  AgentToolRef,
  AgentTurnDisplay,
} from "./agent.ts";
import type { ExternalIdentity } from "./api.ts";

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

export function agentActorFromProto(
  actor?: ProtoAgentActor | undefined,
): AgentActor | undefined {
  if (actor === undefined) {
    return undefined;
  }
  return {
    subjectId: actor.subjectId,
    subjectKind: actor.subjectKind,
    displayName: actor.displayName,
    authSource: actor.authSource,
  };
}

export function agentActorToProto(
  actor?: AgentActor | undefined,
): ProtoAgentActor | undefined {
  if (actor === undefined) {
    return undefined;
  }
  return create(AgentActorSchema, {
    subjectId: actor.subjectId ?? "",
    subjectKind: actor.subjectKind ?? "",
    displayName: actor.displayName ?? "",
    authSource: actor.authSource ?? "",
  });
}

export function agentToolRefFromProto(ref: ProtoAgentToolRef): AgentToolRef {
  return {
    plugin: ref.plugin,
    operation: ref.operation,
    connection: ref.connection,
    instance: ref.instance,
    title: ref.title,
    description: ref.description,
    system: ref.system,
    runAs: agentRunAsSubjectFromProto(ref.runAs),
    runAsExternalIdentity: externalIdentityFromProto(ref.runAsExternalIdentity),
  };
}

export function agentToolRefToProto(ref: AgentToolRef): ProtoAgentToolRef {
  return create(AgentToolRefSchema, {
    plugin: ref.plugin ?? "",
    operation: ref.operation ?? "",
    connection: ref.connection ?? "",
    instance: ref.instance ?? "",
    title: ref.title ?? "",
    description: ref.description ?? "",
    system: ref.system ?? "",
    runAs: agentRunAsSubjectToProto(ref.runAs),
    runAsExternalIdentity: externalIdentityToProto(ref.runAsExternalIdentity),
  });
}

function agentRunAsSubjectFromProto(
  subject?: ProtoAgentSubjectContext | undefined,
): AgentSubjectContext | undefined {
  if (subject === undefined) {
    return undefined;
  }
  return {
    subjectId: subject.subjectId,
    subjectKind: subject.subjectKind,
    credentialSubjectId: subject.credentialSubjectId,
    displayName: subject.displayName,
    authSource: subject.authSource,
  };
}

function agentRunAsSubjectToProto(
  subject?: AgentSubjectContext | undefined,
): ProtoAgentSubjectContext | undefined {
  if (subject === undefined) {
    return undefined;
  }
  return create(AgentSubjectContextSchema, {
    subjectId: subject.subjectId ?? "",
    subjectKind: subject.subjectKind ?? "",
    credentialSubjectId: subject.credentialSubjectId ?? "",
    displayName: subject.displayName ?? "",
    authSource: subject.authSource ?? "",
  });
}

function externalIdentityFromProto(
  identity?: ProtoExternalIdentityContext | undefined,
): ExternalIdentity | undefined {
  if (identity === undefined) {
    return undefined;
  }
  return {
    type: identity.type,
    id: identity.id,
  };
}

function externalIdentityToProto(
  identity?: ExternalIdentity | undefined,
): ProtoExternalIdentityContext | undefined {
  if (identity === undefined) {
    return undefined;
  }
  return create(ExternalIdentityContextSchema, {
    type: identity.type,
    id: identity.id,
  });
}
