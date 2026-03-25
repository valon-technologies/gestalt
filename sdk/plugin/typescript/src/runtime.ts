import * as grpc from "@grpc/grpc-js";
import * as fs from "fs";
import { runtimePluginDefinition, runtimeHostDefinition } from "./grpc-adapter";
import type { RuntimeDefinition, RuntimeHostClient } from "./types";
import type {
  StartRuntimeRequest,
  InvokeRequest,
  OperationResult,
  ListCapabilitiesResponse,
} from "../gen/v1/plugin";
import type { Empty } from "../gen/google/protobuf/empty";

const ENV_PLUGIN_SOCKET = "GESTALT_PLUGIN_SOCKET";
const ENV_RUNTIME_HOST_SOCKET = "GESTALT_RUNTIME_HOST_SOCKET";

type UnaryHandler<Req, Res> = grpc.handleUnaryCall<Req, Res>;

export function dialRuntimeHost(): RuntimeHostClient {
  const socket = process.env[ENV_RUNTIME_HOST_SOCKET];
  if (!socket) {
    throw new Error(
      `${ENV_RUNTIME_HOST_SOCKET} is required. This binary should be launched by the Gestalt host.`,
    );
  }

  const client = new grpc.Client(
    `unix:${socket}`,
    grpc.credentials.createInsecure(),
  );

  const invokeDef = runtimeHostDefinition["Invoke"];
  const listCapsDef = runtimeHostDefinition["ListCapabilities"];

  return {
    invoke(request: InvokeRequest): Promise<OperationResult> {
      return new Promise((resolve, reject) => {
        client.makeUnaryRequest<InvokeRequest, OperationResult>(
          invokeDef.path,
          invokeDef.requestSerialize,
          invokeDef.responseDeserialize,
          request,
          (err: grpc.ServiceError | null, response?: OperationResult) => {
            if (err) reject(err);
            else resolve(response!);
          },
        );
      });
    },

    listCapabilities(): Promise<ListCapabilitiesResponse> {
      const emptyMsg = {} as Empty;
      return new Promise((resolve, reject) => {
        client.makeUnaryRequest<Empty, ListCapabilitiesResponse>(
          listCapsDef.path,
          listCapsDef.requestSerialize,
          listCapsDef.responseDeserialize,
          emptyMsg,
          (
            err: grpc.ServiceError | null,
            response?: ListCapabilitiesResponse,
          ) => {
            if (err) reject(err);
            else resolve(response!);
          },
        );
      });
    },

    close(): void {
      client.close();
    },
  };
}

export function serveRuntime(definition: RuntimeDefinition): void {
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
  let hostClient: RuntimeHostClient | null = null;

  const Start: UnaryHandler<StartRuntimeRequest, Empty> = async (
    call,
    callback,
  ) => {
    try {
      hostClient = dialRuntimeHost();
      await definition.start(call.request, hostClient);
      callback(null, {} as Empty);
    } catch (err: unknown) {
      callback({
        code: grpc.status.UNKNOWN,
        message: `start: ${err instanceof Error ? err.message : String(err)}`,
      });
    }
  };

  const Stop: UnaryHandler<Empty, Empty> = async (_, callback) => {
    try {
      await definition.stop();
      if (hostClient) {
        hostClient.close();
        hostClient = null;
      }
      callback(null, {} as Empty);
    } catch (err: unknown) {
      callback({
        code: grpc.status.UNKNOWN,
        message: `stop: ${err instanceof Error ? err.message : String(err)}`,
      });
    }
  };

  server.addService(runtimePluginDefinition, { Start, Stop });

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
    if (hostClient) {
      hostClient.close();
      hostClient = null;
    }
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
