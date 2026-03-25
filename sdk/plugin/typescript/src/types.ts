import type {
  ProviderMetadata,
  Operation,
  ExecuteRequest,
  OperationResult,
  AuthorizationURLRequest,
  ExchangeCodeRequest,
  RefreshTokenRequest,
  TokenResponse,
  GetSessionCatalogRequest,
  PostConnectRequest,
  StartRuntimeRequest,
  InvokeRequest,
  Capability,
  ListCapabilitiesResponse,
} from "../gen/v1/plugin";

export interface ProviderDefinition {
  metadata: ProviderMetadata;
  operations: Operation[];
  execute: (request: ExecuteRequest) => Promise<OperationResult>;
  auth?: {
    authorizationURL?: (request: AuthorizationURLRequest) => Promise<string>;
    exchangeCode?: (request: ExchangeCodeRequest) => Promise<TokenResponse>;
    refreshToken?: (request: RefreshTokenRequest) => Promise<TokenResponse>;
  };
  sessionCatalog?: (request: GetSessionCatalogRequest) => Promise<string>;
  postConnect?: (request: PostConnectRequest) => Promise<{ [key: string]: string }>;
}

export interface RuntimeHostClient {
  invoke(request: InvokeRequest): Promise<OperationResult>;
  listCapabilities(): Promise<ListCapabilitiesResponse>;
  close(): void;
}

export interface RuntimeDefinition {
  start: (
    request: StartRuntimeRequest,
    host: RuntimeHostClient,
  ) => Promise<void>;
  stop: () => Promise<void>;
}
