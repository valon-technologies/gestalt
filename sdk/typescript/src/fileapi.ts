import { Code, createClient, type Client } from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";

import {
  FileAPI as FileAPIService,
  FileObjectKind,
  LineEndings,
  type BlobPart as ProtoBlobPart,
  type FileObject as ProtoFileObject,
} from "../gen/v1/fileapi_pb.ts";
import type { MaybePromise } from "./api.ts";
import { RuntimeProvider, type RuntimeProviderOptions } from "./provider.ts";

const MAX_SAFE_BIGINT = BigInt(Number.MAX_SAFE_INTEGER);

export const ENV_FILEAPI_SOCKET = "GESTALT_FILEAPI_SOCKET";

export function fileAPISocketEnv(name?: string): string {
  const trimmed = name?.trim() ?? "";
  if (!trimmed) return ENV_FILEAPI_SOCKET;
  return `${ENV_FILEAPI_SOCKET}_${trimmed.replace(/[^A-Za-z0-9]/g, "_").toUpperCase()}`;
}

export class NotFoundError extends Error {
  constructor(message?: string) {
    super(message ?? "not found");
    this.name = "NotFoundError";
  }
}

export type LineEndingMode = "transparent" | "native";

export interface CreateBlobOptions {
  mimeType?: string;
  endings?: LineEndingMode;
}

export interface CreateFileOptions extends CreateBlobOptions {
  lastModified?: number;
}

export type FileAPIPart =
  | string
  | Uint8Array
  | ArrayBuffer
  | ArrayBufferView
  | Blob
  | FileAPIBlob
  | FileAPIFile;

export type FileAPIProviderPart =
  | { kind: "string"; value: string }
  | { kind: "bytes"; value: Uint8Array }
  | { kind: "blob"; id: string };

export interface FileAPISliceRequest {
  id: string;
  start?: number;
  end?: number;
  contentType?: string;
}

interface FileAPIObjectBase {
  id: string;
  size: number;
  type: string;
}

export interface FileAPIBlobDescriptor extends FileAPIObjectBase {
  kind: "blob";
}

export interface FileAPIFileDescriptor extends FileAPIObjectBase {
  kind: "file";
  name: string;
  lastModified: number;
}

export type FileAPIObjectDescriptor = FileAPIBlobDescriptor | FileAPIFileDescriptor;

export interface FileAPIProviderOptions extends RuntimeProviderOptions {
  createBlob: (
    parts: FileAPIProviderPart[],
    options: CreateBlobOptions,
  ) => MaybePromise<FileAPIBlobDescriptor>;
  createFile: (
    fileBits: FileAPIProviderPart[],
    fileName: string,
    options: CreateFileOptions,
  ) => MaybePromise<FileAPIFileDescriptor>;
  stat: (id: string) => MaybePromise<FileAPIObjectDescriptor>;
  slice: (request: FileAPISliceRequest) => MaybePromise<FileAPIBlobDescriptor>;
  readBytes: (id: string) => MaybePromise<Uint8Array>;
  openReadStream: (id: string) => MaybePromise<AsyncIterable<Uint8Array>>;
  createObjectURL: (id: string) => MaybePromise<string>;
  resolveObjectURL: (url: string) => MaybePromise<FileAPIObjectDescriptor>;
  revokeObjectURL: (url: string) => MaybePromise<void>;
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

  async createBlob(
    parts: FileAPIProviderPart[],
    options: CreateBlobOptions,
  ): Promise<FileAPIBlobDescriptor> {
    return await this.createBlobHandler(parts, options);
  }

  async createFile(
    fileBits: FileAPIProviderPart[],
    fileName: string,
    options: CreateFileOptions,
  ): Promise<FileAPIFileDescriptor> {
    return await this.createFileHandler(fileBits, fileName, options);
  }

  async stat(id: string): Promise<FileAPIObjectDescriptor> {
    return await this.statHandler(id);
  }

  async slice(request: FileAPISliceRequest): Promise<FileAPIBlobDescriptor> {
    return await this.sliceHandler(request);
  }

  async readBytes(id: string): Promise<Uint8Array> {
    return await this.readBytesHandler(id);
  }

  async openReadStream(id: string): Promise<AsyncIterable<Uint8Array>> {
    return await this.openReadStreamHandler(id);
  }

  async createObjectURL(id: string): Promise<string> {
    return await this.createObjectURLHandler(id);
  }

  async resolveObjectURL(url: string): Promise<FileAPIObjectDescriptor> {
    return await this.resolveObjectURLHandler(url);
  }

  async revokeObjectURL(url: string): Promise<void> {
    await this.revokeObjectURLHandler(url);
  }
}

export function defineFileAPIProvider(options: FileAPIProviderOptions): FileAPIProvider {
  return new FileAPIProvider(options);
}

export function isFileAPIProvider(value: unknown): value is FileAPIProvider {
  return (
    value instanceof FileAPIProvider ||
    (typeof value === "object" &&
      value !== null &&
      "kind" in value &&
      (value as { kind?: unknown }).kind === "fileapi" &&
      "createBlob" in value &&
      "createFile" in value &&
      "stat" in value &&
      "slice" in value &&
      "readBytes" in value &&
      "openReadStream" in value &&
      "createObjectURL" in value &&
      "resolveObjectURL" in value &&
      "revokeObjectURL" in value)
  );
}

export class FileAPIBlob {
  readonly kind: "blob" | "file" = "blob";
  readonly id: string;
  readonly size: number;
  readonly type: string;

  constructor(
    readonly api: FileAPI,
    descriptor: FileAPIObjectBase,
  ) {
    this.id = descriptor.id;
    this.size = descriptor.size;
    this.type = descriptor.type;
  }

  async stat(): Promise<FileAPIBlob | FileAPIFile> {
    return await this.api.stat(this.id);
  }

  async slice(start?: number, end?: number, contentType?: string): Promise<FileAPIBlob> {
    const request: FileAPISliceRequest = { id: this.id };
    if (start !== undefined) {
      request.start = start;
    }
    if (end !== undefined) {
      request.end = end;
    }
    if (contentType !== undefined) {
      request.contentType = contentType;
    }
    return await this.api.slice(request);
  }

  async readBytes(): Promise<Uint8Array> {
    return await this.api.readBytes(this.id);
  }

  openReadStream(): AsyncIterable<Uint8Array> {
    return this.api.openReadStream(this.id);
  }

  async createObjectURL(): Promise<string> {
    return await this.api.createObjectURL(this.id);
  }

  async bytes(): Promise<Uint8Array> {
    return await this.readBytes();
  }

  async arrayBuffer(): Promise<ArrayBuffer> {
    return arrayBufferFromBytes(await this.readBytes());
  }

  async text(): Promise<string> {
    return new TextDecoder().decode(await this.readBytes());
  }

  async dataURL(): Promise<string> {
    return dataURLFromBytes(await this.readBytes(), this.type);
  }

  stream(): ReadableStream<Uint8Array> {
    return readableStreamFromAsyncIterable(this.openReadStream());
  }
}

export class FileAPIFile extends FileAPIBlob {
  readonly kind = "file" as const;
  readonly name: string;
  readonly lastModified: number;

  constructor(
    api: FileAPI,
    descriptor: FileAPIObjectBase & { name: string; lastModified: number },
  ) {
    super(api, descriptor);
    this.name = descriptor.name;
    this.lastModified = descriptor.lastModified;
  }
}

export class FileAPI {
  private client: Client<typeof FileAPIService>;

  constructor(name?: string) {
    const envName = fileAPISocketEnv(name);
    const socketPath = process.env[envName];
    if (!socketPath) {
      throw new Error(`${envName} is not set`);
    }
    const transport = createGrpcTransport({
      baseUrl: "http://localhost",
      nodeOptions: { path: socketPath },
    });
    this.client = createClient(FileAPIService, transport);
  }

  async createBlob(parts: FileAPIPart[], options: CreateBlobOptions = {}): Promise<FileAPIBlob> {
    const response = await rpc(async () =>
      this.client.createBlob({
        parts: await Promise.all(parts.map((part) => toProtoBlobPart(part))),
        options: {
          mimeType: normalizeString(options.mimeType),
          endings: lineEndingModeToProto(options.endings),
        },
      }),
    );
    return toBlob(fileObjectFromProto(this, response.object));
  }

  async createFile(
    fileBits: FileAPIPart[],
    fileName: string,
    options: CreateFileOptions = {},
  ): Promise<FileAPIFile> {
    const response = await rpc(async () =>
      this.client.createFile({
        fileBits: await Promise.all(fileBits.map((part) => toProtoBlobPart(part))),
        fileName,
        options: {
          mimeType: normalizeString(options.mimeType),
          endings: lineEndingModeToProto(options.endings),
          lastModified: toProtoInt64(options.lastModified ?? Date.now()),
        },
      }),
    );
    return toFile(fileObjectFromProto(this, response.object));
  }

  async stat(id: string): Promise<FileAPIBlob | FileAPIFile> {
    const response = await rpc(() => this.client.stat({ id }));
    return fileObjectFromProto(this, response.object);
  }

  async slice(request: FileAPISliceRequest): Promise<FileAPIBlob> {
    const protoRequest: {
      id: string;
      start?: bigint;
      end?: bigint;
      contentType: string;
    } = {
      id: request.id,
      contentType: normalizeString(request.contentType),
    };
    if (request.start !== undefined) {
      protoRequest.start = toProtoInt64(request.start);
    }
    if (request.end !== undefined) {
      protoRequest.end = toProtoInt64(request.end);
    }
    const response = await rpc(() => this.client.slice(protoRequest));
    return toBlob(fileObjectFromProto(this, response.object));
  }

  async readBytes(id: string): Promise<Uint8Array> {
    const response = await rpc(() => this.client.readBytes({ id }));
    return new Uint8Array(response.data);
  }

  openReadStream(id: string): AsyncIterable<Uint8Array> {
    const stream = this.client.openReadStream({ id });
    return (async function* () {
      for await (const chunk of stream) {
        yield new Uint8Array(chunk.data);
      }
    })();
  }

  async createObjectURL(id: string): Promise<string> {
    const response = await rpc(() => this.client.createObjectURL({ id }));
    return response.url;
  }

  async resolveObjectURL(url: string): Promise<FileAPIBlob | FileAPIFile> {
    const response = await rpc(() => this.client.resolveObjectURL({ url }));
    return fileObjectFromProto(this, response.object);
  }

  async revokeObjectURL(url: string): Promise<void> {
    await rpc(() => this.client.revokeObjectURL({ url }));
  }
}

export function lineEndingModeToProto(value?: LineEndingMode): LineEndings {
  switch (value) {
    case "native":
      return LineEndings.NATIVE;
    case "transparent":
    default:
      return LineEndings.TRANSPARENT;
  }
}

export function lineEndingModeFromProto(value: LineEndings): LineEndingMode {
  switch (value) {
    case LineEndings.NATIVE:
      return "native";
    case LineEndings.TRANSPARENT:
    default:
      return "transparent";
  }
}

export function fileAPIPartFromProto(part: ProtoBlobPart): FileAPIProviderPart {
  switch (part.kind?.case) {
    case "stringData":
      return { kind: "string", value: String(part.kind.value ?? "") };
    case "bytesData":
      return { kind: "bytes", value: new Uint8Array(part.kind.value as Uint8Array) };
    case "blobId":
      return { kind: "blob", id: String(part.kind.value ?? "") };
    default:
      throw new Error("fileapi blob part kind is required");
  }
}

export function fileObjectToProto(descriptor: FileAPIObjectDescriptor): ProtoFileObject {
  return {
    id: descriptor.id,
    kind: descriptor.kind === "file" ? FileObjectKind.FILE : FileObjectKind.BLOB,
    size: toProtoInt64(descriptor.size),
    type: descriptor.type,
    name: descriptor.kind === "file" ? descriptor.name : "",
    lastModified: descriptor.kind === "file" ? toProtoInt64(descriptor.lastModified) : 0n,
  } as ProtoFileObject;
}

export function fileObjectFromProto(
  api: FileAPI,
  object?: ProtoFileObject,
): FileAPIBlob | FileAPIFile {
  if (!object) {
    throw new Error("fileapi response did not include an object");
  }

  const base = {
    id: object.id,
    size: toSafeNumber(object.size, "file size"),
    type: object.type,
  };

  if (object.kind === FileObjectKind.FILE) {
    return new FileAPIFile(api, {
      ...base,
      name: object.name,
      lastModified: toSafeNumber(object.lastModified, "file lastModified"),
    });
  }

  return new FileAPIBlob(api, base);
}

async function toProtoBlobPart(
  part: FileAPIPart,
): Promise<
  | { kind: { case: "stringData"; value: string } }
  | { kind: { case: "bytesData"; value: Uint8Array } }
  | { kind: { case: "blobId"; value: string } }
> {
  if (part instanceof FileAPIBlob || part instanceof FileAPIFile) {
    return { kind: { case: "blobId", value: part.id } };
  }
  if (typeof part === "string") {
    return { kind: { case: "stringData", value: part } };
  }
  if (part instanceof Uint8Array) {
    return { kind: { case: "bytesData", value: new Uint8Array(part) } };
  }
  if (part instanceof ArrayBuffer) {
    return { kind: { case: "bytesData", value: new Uint8Array(part) } };
  }
  if (ArrayBuffer.isView(part)) {
    return { kind: { case: "bytesData", value: bytesFromView(part) } };
  }
  if (part instanceof Blob) {
    return { kind: { case: "bytesData", value: new Uint8Array(await part.arrayBuffer()) } };
  }
  throw new Error(`unsupported FileAPI part: ${describeValue(part)}`);
}

function bytesFromView(view: ArrayBufferView): Uint8Array {
  return new Uint8Array(view.buffer.slice(view.byteOffset, view.byteOffset + view.byteLength));
}

function arrayBufferFromBytes(bytes: Uint8Array): ArrayBuffer {
  return new Uint8Array(bytes).buffer;
}

function dataURLFromBytes(bytes: Uint8Array, mimeType: string): string {
  const prefix = `data:${mimeType || "application/octet-stream"};base64,`;
  return `${prefix}${Buffer.from(bytes).toString("base64")}`;
}

function readableStreamFromAsyncIterable(source: AsyncIterable<Uint8Array>): ReadableStream<Uint8Array> {
  const iterator = source[Symbol.asyncIterator]();
  return new ReadableStream<Uint8Array>({
    async pull(controller) {
      const next = await iterator.next();
      if (next.done) {
        controller.close();
        return;
      }
      controller.enqueue(next.value);
    },
    async cancel(reason) {
      await iterator.return?.(reason);
    },
  });
}

function normalizeString(value: string | undefined): string {
  return value?.trim() ?? "";
}

function toProtoInt64(value: number): bigint {
  if (!Number.isFinite(value)) {
    return 0n;
  }
  return BigInt(Math.trunc(value));
}

function toSafeNumber(value: bigint, label: string): number {
  if (value < -MAX_SAFE_BIGINT || value > MAX_SAFE_BIGINT) {
    throw new Error(`${label} exceeds JavaScript safe integer range`);
  }
  return Number(value);
}

function toBlob(object: FileAPIBlob | FileAPIFile): FileAPIBlob {
  if (object instanceof FileAPIFile) {
    return new FileAPIBlob(object.api, {
      id: object.id,
      size: object.size,
      type: object.type,
    });
  }
  return object;
}

function toFile(object: FileAPIBlob | FileAPIFile): FileAPIFile {
  if (!(object instanceof FileAPIFile)) {
    throw new Error("fileapi response did not describe a file");
  }
  return object;
}

function describeValue(value: unknown): string {
  if (value === null) return "null";
  if (value === undefined) return "undefined";
  if (typeof value === "object" && value.constructor?.name) {
    return value.constructor.name;
  }
  return typeof value;
}

async function rpc<T>(fn: () => Promise<T>): Promise<T> {
  try {
    return await fn();
  } catch (err: any) {
    if (err?.code === Code.NotFound) {
      throw new NotFoundError(err.message);
    }
    throw err;
  }
}

export const FileAPIProviderService = FileAPIService;
