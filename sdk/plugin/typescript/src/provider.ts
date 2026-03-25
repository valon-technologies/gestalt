import * as grpc from "@grpc/grpc-js";
import * as fs from "fs";
import { providerPluginDefinition } from "./grpc-adapter";
import type { ProviderDefinition } from "./types";
import type {
  ExecuteRequest,
  OperationResult,
  ProviderMetadata,
  ListOperationsResponse,
  AuthorizationURLRequest,
  AuthorizationURLResponse,
  ExchangeCodeRequest,
  TokenResponse,
  RefreshTokenRequest,
  GetSessionCatalogRequest,
  GetSessionCatalogResponse,
  PostConnectRequest,
  PostConnectResponse,
} from "../gen/v1/plugin";
import type { Empty } from "../gen/google/protobuf/empty";

const ENV_PLUGIN_SOCKET = "GESTALT_PLUGIN_SOCKET";

type UnaryHandler<Req, Res> = grpc.handleUnaryCall<Req, Res>;

export function serveProvider(definition: ProviderDefinition): void {
  const socket = process.env[ENV_PLUGIN_SOCKET];
  if (!socket) {
    console.error(
      `${ENV_PLUGIN_SOCKET} is required. This binary should be launched by the Gestalt host.`,
    );
    process.exit(1);
  }

  try {
    fs.unlinkSync(socket);
  } catch (err: unknown) {
    if ((err as NodeJS.ErrnoException).code !== "ENOENT") {
      throw err;
    }
  }

  const server = new grpc.Server();

  const GetMetadata: UnaryHandler<Empty, ProviderMetadata> = (_, callback) => {
    callback(null, definition.metadata);
  };

  const ListOperations: UnaryHandler<Empty, ListOperationsResponse> = (
    _,
    callback,
  ) => {
    callback(null, { operations: definition.operations });
  };

  const Execute: UnaryHandler<ExecuteRequest, OperationResult> = async (
    call,
    callback,
  ) => {
    try {
      const result = await definition.execute(call.request);
      callback(null, result);
    } catch (err: unknown) {
      callback({
        code: grpc.status.UNKNOWN,
        message: `execute: ${err instanceof Error ? err.message : String(err)}`,
      });
    }
  };

  const AuthorizationURL: UnaryHandler<
    AuthorizationURLRequest,
    AuthorizationURLResponse
  > = async (call, callback) => {
    if (!definition.auth?.authorizationURL) {
      callback({
        code: grpc.status.UNIMPLEMENTED,
        message: "provider does not support OAuth",
      });
      return;
    }
    try {
      const url = await definition.auth.authorizationURL(call.request);
      callback(null, { url });
    } catch (err: unknown) {
      callback({
        code: grpc.status.UNKNOWN,
        message: `authorization url: ${err instanceof Error ? err.message : String(err)}`,
      });
    }
  };

  const ExchangeCode: UnaryHandler<ExchangeCodeRequest, TokenResponse> =
    async (call, callback) => {
      if (!definition.auth?.exchangeCode) {
        callback({
          code: grpc.status.UNIMPLEMENTED,
          message: "provider does not support OAuth",
        });
        return;
      }
      try {
        const resp = await definition.auth.exchangeCode(call.request);
        callback(null, resp);
      } catch (err: unknown) {
        callback({
          code: grpc.status.UNKNOWN,
          message: `exchange code: ${err instanceof Error ? err.message : String(err)}`,
        });
      }
    };

  const RefreshToken: UnaryHandler<RefreshTokenRequest, TokenResponse> =
    async (call, callback) => {
      if (!definition.auth?.refreshToken) {
        callback({
          code: grpc.status.UNIMPLEMENTED,
          message: "provider does not support OAuth",
        });
        return;
      }
      try {
        const resp = await definition.auth.refreshToken(call.request);
        callback(null, resp);
      } catch (err: unknown) {
        callback({
          code: grpc.status.UNKNOWN,
          message: `refresh token: ${err instanceof Error ? err.message : String(err)}`,
        });
      }
    };

  const GetSessionCatalog: UnaryHandler<
    GetSessionCatalogRequest,
    GetSessionCatalogResponse
  > = async (call, callback) => {
    if (!definition.sessionCatalog) {
      callback({
        code: grpc.status.UNIMPLEMENTED,
        message: "provider does not support session catalogs",
      });
      return;
    }
    try {
      const catalogJson = await definition.sessionCatalog(call.request);
      callback(null, { catalogJson });
    } catch (err: unknown) {
      callback({
        code: grpc.status.UNKNOWN,
        message: `session catalog: ${err instanceof Error ? err.message : String(err)}`,
      });
    }
  };

  const PostConnect: UnaryHandler<PostConnectRequest, PostConnectResponse> =
    async (call, callback) => {
      if (!definition.postConnect) {
        callback({
          code: grpc.status.UNIMPLEMENTED,
          message: "provider does not support post-connect",
        });
        return;
      }
      try {
        const metadata = await definition.postConnect(call.request);
        callback(null, { metadata });
      } catch (err: unknown) {
        callback({
          code: grpc.status.UNKNOWN,
          message: `post-connect: ${err instanceof Error ? err.message : String(err)}`,
        });
      }
    };

  server.addService(providerPluginDefinition, {
    GetMetadata,
    ListOperations,
    Execute,
    AuthorizationURL,
    ExchangeCode,
    RefreshToken,
    GetSessionCatalog,
    PostConnect,
  });

  server.bindAsync(
    `unix:${socket}`,
    grpc.ServerCredentials.createInsecure(),
    (err) => {
      if (err) {
        console.error(`Failed to bind to socket ${socket}: ${err.message}`);
        process.exit(1);
      }
    },
  );

  const shutdown = () => {
    server.tryShutdown((err) => {
      try {
        fs.unlinkSync(socket);
      } catch {
        // ignore
      }
      if (err) {
        console.error(`Error during shutdown: ${err.message}`);
        process.exit(1);
      }
      process.exit(0);
    });
  };

  process.on("SIGTERM", shutdown);
  process.on("SIGINT", shutdown);
}
