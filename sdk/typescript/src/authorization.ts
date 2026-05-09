import { connect } from "node:net";

import type { JsonObject, MessageInitShape } from "@bufbuild/protobuf";
import {
  createClient,
  type Client,
  type Interceptor,
} from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";

import {
  type AccessDecision,
  type ActionSearchResponse,
  type AuthorizationMetadata,
  AuthorizationProvider as AuthorizationProviderService,
  type EffectiveSubjectSearchResponse,
  type ExpandResponse,
  type ReadRelationshipsResponse,
  type Relationship,
  type RelationshipKey,
  type RelationshipTarget,
  type ResourceSearchResponse,
  type SubjectSet,
  type SubjectSearchResponse,
  AccessEvaluationRequestSchema,
  ActionSearchRequestSchema,
  ActionSchema,
  EffectiveSubjectSearchRequestSchema,
  ExpandRequestSchema,
  ReadRelationshipsRequestSchema,
  RelationshipKeySchema,
  RelationshipSchema,
  RelationshipTargetSchema,
  ResourceSchema,
  ResourceSearchRequestSchema,
  SubjectSchema,
  SubjectSetSchema,
  SubjectSearchRequestSchema,
  WriteRelationshipsRequestSchema,
} from "./internal/gen/v1/authorization_pb.ts";

/**
 * Environment variable containing the Unix socket path or relay target for the
 * host authorization client exposed to plugins.
 */
export const ENV_AUTHORIZATION_SOCKET = "GESTALT_AUTHORIZATION_SOCKET";
export const ENV_AUTHORIZATION_SOCKET_TOKEN =
  `${ENV_AUTHORIZATION_SOCKET}_TOKEN`;
const AUTHORIZATION_RELAY_TOKEN_HEADER =
  "x-gestalt-host-service-relay-token";

/** Subject type used for canonical Gestalt subject ids in managed grants. */
export const AUTHORIZATION_SUBJECT_TYPE_SUBJECT = "subject";
/** Managed authorization resource type for agent sessions. */
export const AGENT_SESSION_RESOURCE_TYPE = "agent_session";
/** Relation that grants view and edit access to an agent session. */
export const AGENT_SESSION_RELATION_EDITOR = "editor";
/** Action checked when reading a shared agent session. */
export const AGENT_SESSION_ACTION_VIEW = "view";
/** Action checked when creating turns or resolving interactions in a session. */
export const AGENT_SESSION_ACTION_EDIT = "edit";

export type AuthorizationEvaluateInput = MessageInitShape<
  typeof AccessEvaluationRequestSchema
>;
export type AuthorizationSearchResourcesInput = MessageInitShape<
  typeof ResourceSearchRequestSchema
>;
export type AuthorizationSearchSubjectsInput = MessageInitShape<
  typeof SubjectSearchRequestSchema
>;
export type AuthorizationEffectiveSearchSubjectsInput = MessageInitShape<
  typeof EffectiveSubjectSearchRequestSchema
>;
export type AuthorizationSearchActionsInput = MessageInitShape<
  typeof ActionSearchRequestSchema
>;
export type AuthorizationExpandInput = MessageInitShape<
  typeof ExpandRequestSchema
>;
export type AuthorizationReadRelationshipsInput = MessageInitShape<
  typeof ReadRelationshipsRequestSchema
>;
export type AuthorizationWriteRelationshipsInput = MessageInitShape<
  typeof WriteRelationshipsRequestSchema
>;
export type AuthorizationSubject = MessageInitShape<typeof SubjectSchema>;
export type AuthorizationResource = MessageInitShape<typeof ResourceSchema>;
export type AuthorizationSubjectSet = MessageInitShape<typeof SubjectSetSchema>;
export type AuthorizationRelationshipTarget = MessageInitShape<
  typeof RelationshipTargetSchema
>;
export type AuthorizationAction = MessageInitShape<typeof ActionSchema>;
export type AuthorizationRelationship = MessageInitShape<
  typeof RelationshipSchema
>;
export type AuthorizationRelationshipKey = MessageInitShape<
  typeof RelationshipKeySchema
>;

export type AuthorizationDecisionMessage = AccessDecision;
export type AuthorizationMetadataMessage = AuthorizationMetadata;
export type AuthorizationResourceSearchMessage = ResourceSearchResponse;
export type AuthorizationSubjectSearchMessage = SubjectSearchResponse;
export type AuthorizationEffectiveSubjectSearchMessage =
  EffectiveSubjectSearchResponse;
export type AuthorizationActionSearchMessage = ActionSearchResponse;
export type AuthorizationExpandMessage = ExpandResponse;
export type AuthorizationReadRelationshipsMessage = ReadRelationshipsResponse;
export type AuthorizationRelationshipMessage = Relationship;
export type AuthorizationRelationshipKeyMessage = RelationshipKey;
export type AuthorizationRelationshipTargetMessage = RelationshipTarget;
export type AuthorizationSubjectSetMessage = SubjectSet;

const sharedAuthorizationTransport: {
  target: string;
  token: string;
  client: AuthorizationClient | undefined;
} = {
  target: "",
  token: "",
  client: undefined,
};

/**
 * Client for the host-configured authorization provider.
 *
 * The client accepts plain SDK request objects and performs protobuf message
 * construction inside the transport layer, so callers do not need to import
 * generated protocol modules.
 */
export class AuthorizationClient {
  private readonly client: Client<typeof AuthorizationProviderService>;

  constructor(
    socketTarget?: string,
    relayToken = process.env[ENV_AUTHORIZATION_SOCKET_TOKEN]?.trim() ?? "",
  ) {
    const resolvedTarget = resolveAuthorizationSocketTarget(socketTarget);
    const transportOptions = authorizationTransportOptions(resolvedTarget);
    const transport = createGrpcTransport({
      ...transportOptions,
      ...(transportOptions.nodeOptions
        ? {
            nodeOptions: {
              createConnection: () =>
                connect({ path: transportOptions.nodeOptions!.path }),
            },
          }
        : {}),
      interceptors: relayToken
        ? [authorizationRelayTokenInterceptor(relayToken)]
        : [],
    });
    this.client = createClient(AuthorizationProviderService, transport);
  }

  async evaluate(
    request: AuthorizationEvaluateInput,
  ): Promise<AuthorizationDecisionMessage> {
    return await this.client.evaluate(request);
  }

  async searchResources(
    request: AuthorizationSearchResourcesInput,
  ): Promise<AuthorizationResourceSearchMessage> {
    return await this.client.searchResources(request);
  }

  async searchSubjects(
    request: AuthorizationSearchSubjectsInput,
  ): Promise<AuthorizationSubjectSearchMessage> {
    return await this.client.searchSubjects(request);
  }

  async effectiveSearchResources(
    request: AuthorizationSearchResourcesInput,
  ): Promise<AuthorizationResourceSearchMessage> {
    return await this.client.effectiveSearchResources(request);
  }

  async effectiveSearchSubjects(
    request: AuthorizationEffectiveSearchSubjectsInput,
  ): Promise<AuthorizationEffectiveSubjectSearchMessage> {
    return await this.client.effectiveSearchSubjects(request);
  }

  async searchActions(
    request: AuthorizationSearchActionsInput,
  ): Promise<AuthorizationActionSearchMessage> {
    return await this.client.searchActions(request);
  }

  async expand(
    request: AuthorizationExpandInput,
  ): Promise<AuthorizationExpandMessage> {
    return await this.client.expand(request);
  }

  async readRelationships(
    request: AuthorizationReadRelationshipsInput,
  ): Promise<AuthorizationReadRelationshipsMessage> {
    return await this.client.readRelationships(request);
  }

  /** Writes and deletes authorization relationships. */
  async writeRelationships(
    request: AuthorizationWriteRelationshipsInput,
  ): Promise<void> {
    await this.client.writeRelationships(request);
  }

  /**
   * Grants a canonical Gestalt subject id editor access to an agent session.
   *
   * This writes the host-managed `agent_session` relationship without requiring
   * callers to import generated protobuf modules.
   */
  async grantAgentSessionEditor(
    subjectId: string,
    sessionId: string,
  ): Promise<void> {
    await this.writeRelationships(
      agentSessionEditorWriteRequest(subjectId, sessionId),
    );
  }

  async getMetadata(): Promise<AuthorizationMetadataMessage> {
    return await this.client.getMetadata({});
  }
}

/**
 * Returns a shared host authorization client for authored providers.
 */
export function Authorization(): AuthorizationClient {
  const target = resolveAuthorizationSocketTarget();
  const token = process.env[ENV_AUTHORIZATION_SOCKET_TOKEN]?.trim() ?? "";
  if (
    sharedAuthorizationTransport.client &&
    sharedAuthorizationTransport.target === target &&
    sharedAuthorizationTransport.token === token
  ) {
    return sharedAuthorizationTransport.client;
  }

  const client = new AuthorizationClient(target, token);
  sharedAuthorizationTransport.target = target;
  sharedAuthorizationTransport.token = token;
  sharedAuthorizationTransport.client = client;
  return client;
}

/** Creates an authorization subject reference. */
export function authorizationSubject(
  type: string,
  id: string,
  properties?: JsonObject,
): AuthorizationSubject {
  return properties === undefined ? { type, id } : { type, id, properties };
}

/** Creates an authorization resource reference. */
export function authorizationResource(
  type: string,
  id: string,
  properties?: JsonObject,
): AuthorizationResource {
  return properties === undefined ? { type, id } : { type, id, properties };
}

/** Creates an authorization subject-set reference. */
export function authorizationSubjectSet(
  resource: AuthorizationResource,
  relation: string,
): AuthorizationSubjectSet {
  return { resource, relation };
}

/** Creates a relationship target from a subject. */
export function authorizationSubjectTarget(
  subject: AuthorizationSubject,
): AuthorizationRelationshipTarget {
  return { kind: { case: "subject", value: subject } };
}

/** Creates a relationship target from a resource. */
export function authorizationResourceTarget(
  resource: AuthorizationResource,
): AuthorizationRelationshipTarget {
  return { kind: { case: "resource", value: resource } };
}

/** Creates a relationship target from a subject set. */
export function authorizationSubjectSetTarget(
  resource: AuthorizationResource,
  relation: string,
): AuthorizationRelationshipTarget {
  return {
    kind: {
      case: "subjectSet",
      value: authorizationSubjectSet(resource, relation),
    },
  };
}

/** Creates the managed authorization resource for an agent session. */
export function agentSessionAuthorizationResource(
  sessionId: string,
): AuthorizationResource {
  return authorizationResource(AGENT_SESSION_RESOURCE_TYPE, sessionId);
}

/** Creates an authorization action reference. */
export function authorizationAction(
  name: string,
  properties?: JsonObject,
): AuthorizationAction {
  return properties === undefined ? { name } : { name, properties };
}

/** Creates a relationship tuple for authorization writes. */
export function authorizationRelationship(
  subject: AuthorizationSubject,
  relation: string,
  resource: AuthorizationResource,
  properties?: JsonObject,
): AuthorizationRelationship {
  return properties === undefined
    ? { subject, relation, resource }
    : { subject, relation, resource, properties };
}

/** Creates a generalized relationship tuple for authorization writes. */
export function authorizationRelationshipWithTarget(
  target: AuthorizationRelationshipTarget,
  relation: string,
  resource: AuthorizationResource,
  properties?: JsonObject,
): AuthorizationRelationship {
  return properties === undefined
    ? { target, relation, resource }
    : { target, relation, resource, properties };
}

/**
 * Creates the relationship that shares an agent session with a canonical
 * Gestalt subject id such as `user:123`.
 */
export function agentSessionEditorRelationship(
  subjectId: string,
  sessionId: string,
): AuthorizationRelationship {
  return authorizationRelationshipWithTarget(
    authorizationSubjectTarget(
      authorizationSubject(AUTHORIZATION_SUBJECT_TYPE_SUBJECT, subjectId),
    ),
    AGENT_SESSION_RELATION_EDITOR,
    agentSessionAuthorizationResource(sessionId),
  );
}

/** Creates a relationship-write request that shares an agent session. */
export function agentSessionEditorWriteRequest(
  subjectId: string,
  sessionId: string,
): AuthorizationWriteRelationshipsInput {
  return {
    writes: [agentSessionEditorRelationship(subjectId, sessionId)],
  };
}

/** Creates a relationship key for authorization deletes. */
export function authorizationRelationshipKey(
  subject: AuthorizationSubject,
  relation: string,
  resource: AuthorizationResource,
): AuthorizationRelationshipKey {
  return { subject, relation, resource };
}

/** Creates a generalized relationship key for authorization deletes. */
export function authorizationRelationshipKeyWithTarget(
  target: AuthorizationRelationshipTarget,
  relation: string,
  resource: AuthorizationResource,
): AuthorizationRelationshipKey {
  return { target, relation, resource };
}

function resolveAuthorizationSocketTarget(
  socketPath = process.env[ENV_AUTHORIZATION_SOCKET],
): string {
  const trimmed = socketPath?.trim() ?? "";
  if (!trimmed) {
    throw new Error(`authorization: ${ENV_AUTHORIZATION_SOCKET} is not set`);
  }
  return trimmed;
}

function authorizationTransportOptions(rawTarget: string): {
  baseUrl: string;
  nodeOptions?: { path: string };
} {
  const target = rawTarget.trim();
  if (!target) {
    throw new Error("authorization: transport target is required");
  }
  if (target.startsWith("tcp://")) {
    const address = target.slice("tcp://".length).trim();
    if (!address) {
      throw new Error(
        `authorization: tcp target ${JSON.stringify(rawTarget)} is missing host:port`,
      );
    }
    return { baseUrl: `http://${address}` };
  }
  if (target.startsWith("tls://")) {
    const address = target.slice("tls://".length).trim();
    if (!address) {
      throw new Error(
        `authorization: tls target ${JSON.stringify(rawTarget)} is missing host:port`,
      );
    }
    return { baseUrl: `https://${address}` };
  }
  if (target.startsWith("unix://")) {
    const socketPath = target.slice("unix://".length).trim();
    if (!socketPath) {
      throw new Error(
        `authorization: unix target ${JSON.stringify(rawTarget)} is missing a socket path`,
      );
    }
    return { baseUrl: "http://localhost", nodeOptions: { path: socketPath } };
  }
  if (target.includes("://")) {
    const parsed = new URL(target);
    throw new Error(
      `authorization: unsupported target scheme ${JSON.stringify(parsed.protocol.replace(/:$/, ""))}`,
    );
  }
  return { baseUrl: "http://localhost", nodeOptions: { path: target } };
}

function authorizationRelayTokenInterceptor(token: string): Interceptor {
  return (next) => async (req) => {
    req.header.set(AUTHORIZATION_RELAY_TOKEN_HEADER, token);
    return next(req);
  };
}
