import type { MaybePromise } from "./api.ts";
import {
  isRuntimeProvider,
  RuntimeProvider,
  type RuntimeProviderOptions,
} from "./provider.ts";
import type {
  BytesResponse,
  CreateBlobRequest,
  CreateFileRequest,
  CreateObjectURLRequest,
  FileObjectRequest,
  FileObjectResponse,
  ObjectURLRequest,
  ObjectURLResponse,
  ReadChunk,
  ReadStreamRequest,
  SliceRequest,
} from "../gen/v1/fileapi_pb.ts";

export type FileAPIReadStream =
  | AsyncIterable<ReadChunk>
  | Iterable<ReadChunk>;

export interface FileAPIProviderOptions extends RuntimeProviderOptions {
  createBlob: (
    request: CreateBlobRequest,
  ) => MaybePromise<FileObjectResponse>;
  createFile: (
    request: CreateFileRequest,
  ) => MaybePromise<FileObjectResponse>;
  stat: (request: FileObjectRequest) => MaybePromise<FileObjectResponse>;
  slice: (request: SliceRequest) => MaybePromise<FileObjectResponse>;
  readBytes: (request: FileObjectRequest) => MaybePromise<BytesResponse>;
  openReadStream: (
    request: ReadStreamRequest,
  ) => MaybePromise<FileAPIReadStream>;
  createObjectURL: (
    request: CreateObjectURLRequest,
  ) => MaybePromise<ObjectURLResponse>;
  resolveObjectURL: (
    request: ObjectURLRequest,
  ) => MaybePromise<FileObjectResponse>;
  revokeObjectURL: (request: ObjectURLRequest) => MaybePromise<void>;
}

export class FileAPIProvider extends RuntimeProvider {
  readonly kind = "fileapi" as const;

  private readonly createBlobHandler: FileAPIProviderOptions["createBlob"];
  private readonly createFileHandler: FileAPIProviderOptions["createFile"];
  private readonly statHandler: FileAPIProviderOptions["stat"];
  private readonly sliceHandler: FileAPIProviderOptions["slice"];
  private readonly readBytesHandler: FileAPIProviderOptions["readBytes"];
  private readonly openReadStreamHandler: FileAPIProviderOptions["openReadStream"];
  private readonly createObjectURLHandler: FileAPIProviderOptions["createObjectURL"];
  private readonly resolveObjectURLHandler: FileAPIProviderOptions["resolveObjectURL"];
  private readonly revokeObjectURLHandler: FileAPIProviderOptions["revokeObjectURL"];

  constructor(options: FileAPIProviderOptions) {
    super(options);
    this.createBlobHandler = options.createBlob;
    this.createFileHandler = options.createFile;
    this.statHandler = options.stat;
    this.sliceHandler = options.slice;
    this.readBytesHandler = options.readBytes;
    this.openReadStreamHandler = options.openReadStream;
    this.createObjectURLHandler = options.createObjectURL;
    this.resolveObjectURLHandler = options.resolveObjectURL;
    this.revokeObjectURLHandler = options.revokeObjectURL;
  }

  async createBlob(request: CreateBlobRequest): Promise<FileObjectResponse> {
    return await this.createBlobHandler(request);
  }

  async createFile(request: CreateFileRequest): Promise<FileObjectResponse> {
    return await this.createFileHandler(request);
  }

  async stat(request: FileObjectRequest): Promise<FileObjectResponse> {
    return await this.statHandler(request);
  }

  async slice(request: SliceRequest): Promise<FileObjectResponse> {
    return await this.sliceHandler(request);
  }

  async readBytes(request: FileObjectRequest): Promise<BytesResponse> {
    return await this.readBytesHandler(request);
  }

  async openReadStream(request: ReadStreamRequest): Promise<FileAPIReadStream> {
    return await this.openReadStreamHandler(request);
  }

  async createObjectURL(
    request: CreateObjectURLRequest,
  ): Promise<ObjectURLResponse> {
    return await this.createObjectURLHandler(request);
  }

  async resolveObjectURL(request: ObjectURLRequest): Promise<FileObjectResponse> {
    return await this.resolveObjectURLHandler(request);
  }

  async revokeObjectURL(request: ObjectURLRequest): Promise<void> {
    await this.revokeObjectURLHandler(request);
  }
}

export function defineFileAPIProvider(
  options: FileAPIProviderOptions,
): FileAPIProvider {
  return new FileAPIProvider(options);
}

export function isFileAPIProvider(value: unknown): value is FileAPIProvider {
  return (
    value instanceof FileAPIProvider ||
    (isRuntimeProvider(value) &&
      "kind" in value &&
      (value as { kind?: unknown }).kind === "fileapi" &&
      "createBlob" in value &&
      "openReadStream" in value &&
      "resolveObjectURL" in value &&
      "revokeObjectURL" in value)
  );
}
