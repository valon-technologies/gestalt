import { connect } from "node:net";

import { create } from "@bufbuild/protobuf";
import { EmptySchema } from "@bufbuild/protobuf/wkt";
import {
  Code,
  ConnectError,
  createClient,
  type Client,
  type ServiceImpl,
} from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";

import {
  CopyObjectResponseSchema,
  HeadObjectResponseSchema,
  ListObjectsResponseSchema,
  PresignMethod as ProtoPresignMethod,
  PresignObjectResponseSchema,
  ReadObjectChunkSchema,
  S3 as S3Service,
  WriteObjectResponseSchema,
} from "../gen/v1/s3_pb.ts";
import { errorMessage, type MaybePromise } from "./api.ts";
import { RuntimeProvider, type RuntimeProviderOptions } from "./provider.ts";

const ENV_S3_SOCKET = "GESTALT_S3_SOCKET";
const WRITE_CHUNK_SIZE = 64 * 1024;
const textEncoder = new TextEncoder();

export function s3SocketEnv(name?: string): string {
  const trimmed = name?.trim() ?? "";
  if (!trimmed) return ENV_S3_SOCKET;
  return `${ENV_S3_SOCKET}_${trimmed.replace(/[^A-Za-z0-9]/g, "_").toUpperCase()}`;
}

export class S3NotFoundError extends Error {
  constructor(message?: string) {
    super(message ?? "s3: not found");
    this.name = "S3NotFoundError";
  }
}

export class S3PreconditionFailedError extends Error {
  constructor(message?: string) {
    super(message ?? "s3: precondition failed");
    this.name = "S3PreconditionFailedError";
  }
}

export class S3InvalidRangeError extends Error {
  constructor(message?: string) {
    super(message ?? "s3: invalid range");
    this.name = "S3InvalidRangeError";
  }
}

export interface ObjectRef {
  bucket: string;
  key: string;
  versionId?: string;
}

export interface ObjectMeta {
  ref: ObjectRef;
  etag: string;
  size: bigint;
  contentType: string;
  lastModified?: Date;
  metadata: Record<string, string>;
  storageClass: string;
}

export interface ByteRange {
  start?: number | bigint;
  end?: number | bigint;
}

export interface ReadOptions {
  range?: ByteRange;
  ifMatch?: string;
  ifNoneMatch?: string;
  ifModifiedSince?: Date;
  ifUnmodifiedSince?: Date;
}

export interface WriteOptions {
  contentType?: string;
  cacheControl?: string;
  contentDisposition?: string;
  contentEncoding?: string;
  contentLanguage?: string;
  metadata?: Record<string, string>;
  ifMatch?: string;
  ifNoneMatch?: string;
}

export interface ListOptions {
  bucket: string;
  prefix?: string;
  delimiter?: string;
  continuationToken?: string;
  startAfter?: string;
  maxKeys?: number;
}

export interface ListPage {
  objects: ObjectMeta[];
  commonPrefixes: string[];
  nextContinuationToken: string;
  hasMore: boolean;
}

export interface CopyOptions {
  ifMatch?: string;
  ifNoneMatch?: string;
}

export enum PresignMethod {
  Get = "GET",
  Put = "PUT",
  Delete = "DELETE",
  Head = "HEAD",
}

export interface PresignOptions {
  method?: PresignMethod;
  expiresSeconds?: number | bigint;
  contentType?: string;
  contentDisposition?: string;
  headers?: Record<string, string>;
}

export interface PresignResult {
  url: string;
  method: PresignMethod;
  expiresAt?: Date;
  headers: Record<string, string>;
}

export type S3BodySource =
  | string
  | Uint8Array
  | ArrayBuffer
  | ArrayBufferView
  | Blob
  | ReadableStream<Uint8Array>
  | AsyncIterable<Uint8Array>
  | null
  | undefined;

export interface ReadResult {
  meta: ObjectMeta;
  stream: AsyncIterable<Uint8Array>;
}

export interface ProviderReadResult {
  meta: ObjectMeta;
  body?: S3BodySource;
}

export interface S3ProviderOptions extends RuntimeProviderOptions {
  headObject: (ref: ObjectRef) => MaybePromise<ObjectMeta>;
  readObject: (ref: ObjectRef, options?: ReadOptions) => MaybePromise<ProviderReadResult>;
  writeObject: (
    ref: ObjectRef,
    body: AsyncIterable<Uint8Array>,
    options?: WriteOptions,
  ) => MaybePromise<ObjectMeta>;
  deleteObject: (ref: ObjectRef) => MaybePromise<void>;
  listObjects: (options: ListOptions) => MaybePromise<ListPage>;
  copyObject: (
    source: ObjectRef,
    destination: ObjectRef,
    options?: CopyOptions,
  ) => MaybePromise<ObjectMeta>;
  presignObject: (
    ref: ObjectRef,
    options?: PresignOptions,
  ) => MaybePromise<PresignResult>;
}

export class S3Provider extends RuntimeProvider {
  readonly kind = "s3" as const;

  private readonly headObjectHandler: S3ProviderOptions["headObject"];
  private readonly readObjectHandler: S3ProviderOptions["readObject"];
  private readonly writeObjectHandler: S3ProviderOptions["writeObject"];
  private readonly deleteObjectHandler: S3ProviderOptions["deleteObject"];
  private readonly listObjectsHandler: S3ProviderOptions["listObjects"];
  private readonly copyObjectHandler: S3ProviderOptions["copyObject"];
  private readonly presignObjectHandler: S3ProviderOptions["presignObject"];

  constructor(options: S3ProviderOptions) {
    super(options);
    this.headObjectHandler = options.headObject;
    this.readObjectHandler = options.readObject;
    this.writeObjectHandler = options.writeObject;
    this.deleteObjectHandler = options.deleteObject;
    this.listObjectsHandler = options.listObjects;
    this.copyObjectHandler = options.copyObject;
    this.presignObjectHandler = options.presignObject;
  }

  async headObject(ref: ObjectRef): Promise<ObjectMeta> {
    return await this.headObjectHandler(ref);
  }

  async readObject(ref: ObjectRef, options?: ReadOptions): Promise<ProviderReadResult> {
    return await this.readObjectHandler(ref, options);
  }

  async writeObject(
    ref: ObjectRef,
    body: AsyncIterable<Uint8Array>,
    options?: WriteOptions,
  ): Promise<ObjectMeta> {
    return await this.writeObjectHandler(ref, body, options);
  }

  async deleteObject(ref: ObjectRef): Promise<void> {
    await this.deleteObjectHandler(ref);
  }

  async listObjects(options: ListOptions): Promise<ListPage> {
    return await this.listObjectsHandler(options);
  }

  async copyObject(
    source: ObjectRef,
    destination: ObjectRef,
    options?: CopyOptions,
  ): Promise<ObjectMeta> {
    return await this.copyObjectHandler(source, destination, options);
  }

  async presignObject(ref: ObjectRef, options?: PresignOptions): Promise<PresignResult> {
    return await this.presignObjectHandler(ref, options);
  }
}

export function defineS3Provider(options: S3ProviderOptions): S3Provider {
  return new S3Provider(options);
}

export function isS3Provider(value: unknown): value is S3Provider {
  return (
    value instanceof S3Provider ||
    (typeof value === "object" &&
      value !== null &&
      "kind" in value &&
      (value as { kind?: unknown }).kind === "s3" &&
      "headObject" in value &&
      "readObject" in value &&
      "writeObject" in value &&
      "deleteObject" in value &&
      "listObjects" in value &&
      "copyObject" in value &&
      "presignObject" in value)
  );
}

export function createS3Service(
  provider: S3Provider,
): Partial<ServiceImpl<typeof S3Service>> {
  return {
    async headObject(request) {
      const meta = await invokeS3Provider("head object", () =>
        provider.headObject(fromProtoObjectRef(request.ref)),
      );
      return create(HeadObjectResponseSchema, { meta: toProtoObjectMeta(meta) });
    },
    async *readObject(request) {
      const result = await invokeS3Provider("read object", () =>
        provider.readObject(fromProtoObjectRef(request.ref), fromProtoReadOptions(request)),
      );
      yield create(ReadObjectChunkSchema, {
        result: {
          case: "meta",
          value: toProtoObjectMeta(result.meta),
        },
      });
      try {
        for await (const chunk of toAsyncByteStream(result.body)) {
          if (chunk.byteLength === 0) {
            continue;
          }
          yield create(ReadObjectChunkSchema, {
            result: {
              case: "data",
              value: chunk,
            },
          });
        }
      } catch (error) {
        throw toS3ConnectError(error, "read object");
      }
    },
    async writeObject(request) {
      const iterator = request[Symbol.asyncIterator]();
      const first = await readNextRequest(iterator, "write object");
      if (first.done || first.value.msg.case !== "open") {
        throw new ConnectError(
          "write object stream must begin with an open frame",
          Code.InvalidArgument,
        );
      }
      const open = first.value.msg.value;
      const body = writeBodyFromStream(iterator);
      try {
        const meta = await invokeS3Provider("write object", () =>
          provider.writeObject(
            fromProtoObjectRef(open.ref),
            body,
            fromProtoWriteOptions(open),
          ),
        );
        return create(WriteObjectResponseSchema, {
          meta: toProtoObjectMeta(meta),
        });
      } finally {
        if (typeof body.return === "function") {
          await body.return();
        }
      }
    },
    async deleteObject(request) {
      await invokeS3Provider("delete object", () =>
        provider.deleteObject(fromProtoObjectRef(request.ref)),
      );
      return create(EmptySchema, {});
    },
    async listObjects(request) {
      const options: ListOptions = {
        bucket: request.bucket,
      };
      if (request.prefix) {
        options.prefix = request.prefix;
      }
      if (request.delimiter) {
        options.delimiter = request.delimiter;
      }
      if (request.continuationToken) {
        options.continuationToken = request.continuationToken;
      }
      if (request.startAfter) {
        options.startAfter = request.startAfter;
      }
      if (request.maxKeys > 0) {
        options.maxKeys = request.maxKeys;
      }
      const page = await invokeS3Provider("list objects", () =>
        provider.listObjects(options),
      );
      return create(ListObjectsResponseSchema, {
        objects: page.objects.map(toProtoObjectMeta),
        commonPrefixes: [...page.commonPrefixes],
        nextContinuationToken: page.nextContinuationToken,
        hasMore: page.hasMore,
      });
    },
    async copyObject(request) {
      const options: CopyOptions = {};
      if (request.ifMatch) {
        options.ifMatch = request.ifMatch;
      }
      if (request.ifNoneMatch) {
        options.ifNoneMatch = request.ifNoneMatch;
      }
      const meta = await invokeS3Provider("copy object", () =>
        provider.copyObject(
          fromProtoObjectRef(request.source),
          fromProtoObjectRef(request.destination),
          options,
        ),
      );
      return create(CopyObjectResponseSchema, { meta: toProtoObjectMeta(meta) });
    },
    async presignObject(request) {
      const options: PresignOptions = {
        method: fromProtoPresignMethod(request.method),
        headers: cloneStringMap(request.headers),
      };
      if (request.expiresSeconds !== 0n) {
        options.expiresSeconds = request.expiresSeconds;
      }
      if (request.contentType) {
        options.contentType = request.contentType;
      }
      if (request.contentDisposition) {
        options.contentDisposition = request.contentDisposition;
      }
      const result = await invokeS3Provider("presign object", () =>
        provider.presignObject(fromProtoObjectRef(request.ref), options),
      );
      const response = {
        url: result.url,
        method: toProtoPresignMethod(result.method),
        headers: cloneStringMap(result.headers),
      } as {
        url: string;
        method: ProtoPresignMethod;
        headers: Record<string, string>;
        expiresAt?: { seconds: bigint; nanos: number };
      };
      if (result.expiresAt) {
        response.expiresAt = toProtoTimestamp(result.expiresAt);
      }
      return create(PresignObjectResponseSchema, response);
    },
  };
}

export class S3 {
  private readonly client: Client<typeof S3Service>;

  constructor(name?: string) {
    const envName = s3SocketEnv(name);
    const socketPath = process.env[envName];
    if (!socketPath) {
      throw new Error(`${envName} is not set`);
    }
    const transport = createGrpcTransport({
      baseUrl: "http://localhost",
      nodeOptions: {
        createConnection: () => connect(socketPath),
      },
    });
    this.client = createClient(S3Service, transport);
  }

  object(bucket: string, key: string): S3Object {
    return new S3Object(this, { bucket, key });
  }

  objectVersion(bucket: string, key: string, versionId: string): S3Object {
    return new S3Object(this, { bucket, key, versionId });
  }

  async headObject(ref: ObjectRef): Promise<ObjectMeta> {
    const response = await s3Rpc(() =>
      this.client.headObject({
        ref: toProtoObjectRef(ref),
      }),
    );
    return fromProtoObjectMeta(response.meta);
  }

  async readObject(ref: ObjectRef, options?: ReadOptions): Promise<ReadResult> {
    const response = this.client.readObject({
      ref: toProtoObjectRef(ref),
      ...toProtoReadOptions(options),
    });
    const iterator = response[Symbol.asyncIterator]();
    const first = await readNextResponse(iterator);
    if (first.done || first.value.result.case !== "meta") {
      throw new Error("s3 read stream did not start with object metadata");
    }
    return {
      meta: fromProtoObjectMeta(first.value.result.value),
      stream: readDataChunks(iterator),
    };
  }

  async writeObject(
    ref: ObjectRef,
    body?: S3BodySource,
    options?: WriteOptions,
  ): Promise<ObjectMeta> {
    const snapshot = snapshotS3Body(body);
    const response = await s3Rpc(() =>
      this.client.writeObject(writeRequests(ref, snapshot, options)),
    );
    return fromProtoObjectMeta(response.meta);
  }

  async deleteObject(ref: ObjectRef): Promise<void> {
    await s3Rpc(() =>
      this.client.deleteObject({
        ref: toProtoObjectRef(ref),
      }),
    );
  }

  async listObjects(options: ListOptions): Promise<ListPage> {
    const response = await s3Rpc(() =>
      this.client.listObjects({
        bucket: options.bucket,
        prefix: options.prefix ?? "",
        delimiter: options.delimiter ?? "",
        continuationToken: options.continuationToken ?? "",
        startAfter: options.startAfter ?? "",
        maxKeys: options.maxKeys ?? 0,
      }),
    );
    return {
      objects: response.objects.map(fromProtoObjectMeta),
      commonPrefixes: [...response.commonPrefixes],
      nextContinuationToken: response.nextContinuationToken,
      hasMore: response.hasMore,
    };
  }

  async copyObject(
    source: ObjectRef,
    destination: ObjectRef,
    options?: CopyOptions,
  ): Promise<ObjectMeta> {
    const response = await s3Rpc(() =>
      this.client.copyObject({
        source: toProtoObjectRef(source),
        destination: toProtoObjectRef(destination),
        ifMatch: options?.ifMatch ?? "",
        ifNoneMatch: options?.ifNoneMatch ?? "",
      }),
    );
    return fromProtoObjectMeta(response.meta);
  }

  async presignObject(ref: ObjectRef, options?: PresignOptions): Promise<PresignResult> {
    const requestedMethod = options?.method ?? PresignMethod.Get;
    const response = await s3Rpc(() =>
      this.client.presignObject({
        ref: toProtoObjectRef(ref),
        method: toProtoPresignMethod(requestedMethod),
        expiresSeconds: normalizeProtoInt(options?.expiresSeconds),
        contentType: options?.contentType ?? "",
        contentDisposition: options?.contentDisposition ?? "",
        headers: cloneStringMap(options?.headers),
      }),
    );
    const result: PresignResult = {
      url: response.url,
      method: response.method === ProtoPresignMethod.UNSPECIFIED
        ? requestedMethod
        : fromProtoPresignMethod(response.method),
      headers: cloneStringMap(response.headers),
    };
    if (response.expiresAt) {
      result.expiresAt = fromProtoTimestamp(response.expiresAt);
    }
    return result;
  }
}

export class S3Object {
  constructor(
    private readonly client: S3,
    readonly ref: ObjectRef,
  ) {}

  async stat(): Promise<ObjectMeta> {
    return await this.client.headObject(this.ref);
  }

  async exists(): Promise<boolean> {
    try {
      await this.stat();
      return true;
    } catch (error) {
      if (error instanceof S3NotFoundError) {
        return false;
      }
      throw error;
    }
  }

  async read(options?: ReadOptions): Promise<ReadResult> {
    return await this.client.readObject(this.ref, options);
  }

  async stream(options?: ReadOptions): Promise<AsyncIterable<Uint8Array>> {
    const result = await this.read(options);
    return result.stream;
  }

  async bytes(options?: ReadOptions): Promise<Uint8Array> {
    const result = await this.read(options);
    return await collectBytes(result.stream);
  }

  async text(options?: ReadOptions, encoding = "utf-8"): Promise<string> {
    return new TextDecoder(encoding).decode(await this.bytes(options));
  }

  async json<T = unknown>(options?: ReadOptions): Promise<T> {
    return JSON.parse(await this.text(options)) as T;
  }

  async write(body?: S3BodySource, options?: WriteOptions): Promise<ObjectMeta> {
    return await this.client.writeObject(this.ref, body, options);
  }

  async writeBytes(body: Uint8Array | ArrayBuffer | ArrayBufferView): Promise<ObjectMeta> {
    return await this.write(body);
  }

  async writeString(body: string, options?: WriteOptions): Promise<ObjectMeta> {
    return await this.write(body, options);
  }

  async writeJSON(value: unknown, options: WriteOptions = {}): Promise<ObjectMeta> {
    return await this.write(JSON.stringify(value), {
      ...options,
      contentType: options.contentType ?? "application/json",
    });
  }

  async delete(): Promise<void> {
    await this.client.deleteObject(this.ref);
  }

  async presign(options?: PresignOptions): Promise<PresignResult> {
    return await this.client.presignObject(this.ref, options);
  }
}

async function invokeS3Provider<T>(label: string, fn: () => Promise<T>): Promise<T> {
  try {
    return await fn();
  } catch (error) {
    throw toS3ConnectError(error, label);
  }
}

async function readNextRequest<T>(
  iterator: AsyncIterator<T>,
  label: string,
): Promise<IteratorResult<T>> {
  try {
    return await iterator.next();
  } catch (error) {
    throw toS3ConnectError(error, label);
  }
}

async function readNextResponse<T>(iterator: AsyncIterator<T>): Promise<IteratorResult<T>> {
  try {
    return await iterator.next();
  } catch (error) {
    throw mapS3RpcError(error);
  }
}

async function* readDataChunks(
  iterator: AsyncIterator<{ result: { case: "meta"; value: unknown } | { case: "data"; value: Uint8Array } | { case: undefined; value?: undefined } }>,
): AsyncIterable<Uint8Array> {
  let finished = false;
  try {
    while (true) {
      const next = await readNextResponse(iterator);
      if (next.done) {
        finished = true;
        return;
      }
      if (next.value.result.case !== "data") {
        throw new Error("s3 read stream emitted an unexpected metadata frame");
      }
      yield cloneBytes(next.value.result.value);
    }
  } finally {
    if (!finished && typeof iterator.return === "function") {
      await iterator.return();
    }
  }
}

async function* writeRequests(
  ref: ObjectRef,
  body?: S3BodySource,
  options?: WriteOptions,
): AsyncIterable<{
  msg:
    | { case: "open"; value: Record<string, unknown> }
    | { case: "data"; value: Uint8Array };
}> {
  yield {
    msg: {
      case: "open",
      value: {
        ref: toProtoObjectRef(ref),
        contentType: options?.contentType ?? "",
        cacheControl: options?.cacheControl ?? "",
        contentDisposition: options?.contentDisposition ?? "",
        contentEncoding: options?.contentEncoding ?? "",
        contentLanguage: options?.contentLanguage ?? "",
        metadata: cloneStringMap(options?.metadata),
        ifMatch: options?.ifMatch ?? "",
        ifNoneMatch: options?.ifNoneMatch ?? "",
      },
    },
  };
  for await (const chunk of toAsyncByteStream(body)) {
    if (chunk.byteLength === 0) {
      continue;
    }
    yield {
      msg: {
        case: "data",
        value: chunk,
      },
    };
  }
}

async function* writeBodyFromStream(
  iterator: AsyncIterator<{ msg: { case: "open" | "data" | undefined; value?: any } }>,
): AsyncGenerator<Uint8Array, void, undefined> {
  try {
    while (true) {
      const next = await readNextRequest(iterator, "write object");
      if (next.done) {
        return;
      }
      if (next.value.msg.case !== "data") {
        throw new ConnectError(
          "write object frames after open must carry data",
          Code.InvalidArgument,
        );
      }
      const chunk = cloneBytes(next.value.msg.value as Uint8Array);
      if (chunk.byteLength === 0) {
        continue;
      }
      yield chunk;
    }
  } finally {
    if (typeof iterator.return === "function") {
      await iterator.return();
    }
  }
}

async function collectBytes(stream: AsyncIterable<Uint8Array>): Promise<Uint8Array> {
  const parts: Uint8Array[] = [];
  let total = 0;
  for await (const chunk of stream) {
    parts.push(chunk);
    total += chunk.byteLength;
  }
  const out = new Uint8Array(total);
  let offset = 0;
  for (const part of parts) {
    out.set(part, offset);
    offset += part.byteLength;
  }
  return out;
}

async function* toAsyncByteStream(body?: S3BodySource): AsyncIterable<Uint8Array> {
  if (body == null) {
    return;
  }
  if (typeof body === "string") {
    yield* chunkBytes(textEncoder.encode(body));
    return;
  }
  if (body instanceof Uint8Array) {
    yield* chunkBytes(body);
    return;
  }
  if (body instanceof ArrayBuffer) {
    yield* chunkBytes(new Uint8Array(body));
    return;
  }
  if (ArrayBuffer.isView(body)) {
    yield* chunkBytes(new Uint8Array(body.buffer, body.byteOffset, body.byteLength));
    return;
  }
  if (body instanceof Blob) {
    yield* readableStreamToAsyncIterable(body.stream() as ReadableStream<Uint8Array>);
    return;
  }
  if (isReadableStream(body)) {
    yield* readableStreamToAsyncIterable(body);
    return;
  }
  if (isAsyncIterable(body)) {
    for await (const chunk of body) {
      yield cloneBytes(chunk);
    }
    return;
  }
  throw new Error("unsupported s3 body source");
}

function* chunkBytes(bytes: Uint8Array): Iterable<Uint8Array> {
  for (let offset = 0; offset < bytes.byteLength; offset += WRITE_CHUNK_SIZE) {
    yield cloneBytes(bytes.subarray(offset, offset + WRITE_CHUNK_SIZE));
  }
}

function snapshotS3Body(body?: S3BodySource): S3BodySource | undefined {
  if (body == null || typeof body === "string") {
    return body;
  }
  if (body instanceof Uint8Array) {
    return cloneBytes(body);
  }
  if (body instanceof ArrayBuffer) {
    return body.slice(0);
  }
  if (ArrayBuffer.isView(body)) {
    return new Uint8Array(body.buffer.slice(body.byteOffset, body.byteOffset + body.byteLength));
  }
  return body;
}

async function* readableStreamToAsyncIterable(
  stream: ReadableStream<Uint8Array>,
): AsyncIterable<Uint8Array> {
  const reader = stream.getReader();
  let exhausted = false;
  try {
    while (true) {
      const { value, done } = await reader.read();
      if (done) {
        exhausted = true;
        return;
      }
      if (!value) {
        continue;
      }
      yield cloneBytes(value);
    }
  } finally {
    try {
      if (!exhausted) {
        await reader.cancel();
      }
    } catch {
      // Ignore cancellation failures and preserve the original stream result.
    } finally {
      reader.releaseLock();
    }
  }
}

function isAsyncIterable(value: unknown): value is AsyncIterable<Uint8Array> {
  return typeof value === "object" && value !== null && Symbol.asyncIterator in value;
}

function isReadableStream(value: unknown): value is ReadableStream<Uint8Array> {
  return typeof value === "object" && value !== null && "getReader" in value;
}

function toProtoObjectRef(ref: ObjectRef) {
  return {
    bucket: ref.bucket,
    key: ref.key,
    versionId: ref.versionId ?? "",
  };
}

function fromProtoObjectRef(ref: { bucket?: string; key?: string; versionId?: string } | undefined): ObjectRef {
  const value: ObjectRef = {
    bucket: ref?.bucket ?? "",
    key: ref?.key ?? "",
  };
  if (ref?.versionId) {
    value.versionId = ref.versionId;
  }
  return value;
}

function toProtoObjectMeta(meta: ObjectMeta) {
  const value: {
    ref: ReturnType<typeof toProtoObjectRef>;
    etag: string;
    size: bigint;
    contentType: string;
    metadata: Record<string, string>;
    storageClass: string;
    lastModified?: { seconds: bigint; nanos: number };
  } = {
    ref: toProtoObjectRef(meta.ref),
    etag: meta.etag,
    size: meta.size,
    contentType: meta.contentType,
    metadata: cloneStringMap(meta.metadata),
    storageClass: meta.storageClass,
  };
  if (meta.lastModified) {
    value.lastModified = toProtoTimestamp(meta.lastModified);
  }
  return value;
}

function fromProtoObjectMeta(meta: {
  ref?: { bucket?: string; key?: string; versionId?: string };
  etag?: string;
  size?: bigint;
  contentType?: string;
  lastModified?: { seconds?: bigint; nanos?: number };
  metadata?: Record<string, string>;
  storageClass?: string;
} | undefined): ObjectMeta {
  const value: ObjectMeta = {
    ref: fromProtoObjectRef(meta?.ref),
    etag: meta?.etag ?? "",
    size: meta?.size ?? 0n,
    contentType: meta?.contentType ?? "",
    metadata: cloneStringMap(meta?.metadata),
    storageClass: meta?.storageClass ?? "",
  };
  if (meta?.lastModified) {
    value.lastModified = fromProtoTimestamp(meta.lastModified);
  }
  return value;
}

function toProtoReadOptions(options?: ReadOptions) {
  const proto: Record<string, unknown> = {
    ifMatch: options?.ifMatch ?? "",
    ifNoneMatch: options?.ifNoneMatch ?? "",
  };
  if (options?.range) {
    proto.range = toProtoByteRange(options.range);
  }
  if (options?.ifModifiedSince) {
    proto.ifModifiedSince = toProtoTimestamp(options.ifModifiedSince);
  }
  if (options?.ifUnmodifiedSince) {
    proto.ifUnmodifiedSince = toProtoTimestamp(options.ifUnmodifiedSince);
  }
  return proto;
}

function fromProtoReadOptions(request: {
  range?: { start?: bigint; end?: bigint };
  ifMatch?: string;
  ifNoneMatch?: string;
  ifModifiedSince?: { seconds?: bigint; nanos?: number };
  ifUnmodifiedSince?: { seconds?: bigint; nanos?: number };
}): ReadOptions {
  const options: ReadOptions = {};
  if (request.range) {
    options.range = fromProtoByteRange(request.range);
  }
  if (request.ifMatch) {
    options.ifMatch = request.ifMatch;
  }
  if (request.ifNoneMatch) {
    options.ifNoneMatch = request.ifNoneMatch;
  }
  if (request.ifModifiedSince) {
    options.ifModifiedSince = fromProtoTimestamp(request.ifModifiedSince);
  }
  if (request.ifUnmodifiedSince) {
    options.ifUnmodifiedSince = fromProtoTimestamp(request.ifUnmodifiedSince);
  }
  return options;
}

function fromProtoWriteOptions(open: {
  contentType?: string;
  cacheControl?: string;
  contentDisposition?: string;
  contentEncoding?: string;
  contentLanguage?: string;
  metadata?: Record<string, string>;
  ifMatch?: string;
  ifNoneMatch?: string;
}): WriteOptions {
  const options: WriteOptions = {};
  if (open.contentType) {
    options.contentType = open.contentType;
  }
  if (open.cacheControl) {
    options.cacheControl = open.cacheControl;
  }
  if (open.contentDisposition) {
    options.contentDisposition = open.contentDisposition;
  }
  if (open.contentEncoding) {
    options.contentEncoding = open.contentEncoding;
  }
  if (open.contentLanguage) {
    options.contentLanguage = open.contentLanguage;
  }
  if (open.metadata && Object.keys(open.metadata).length > 0) {
    options.metadata = cloneStringMap(open.metadata);
  }
  if (open.ifMatch) {
    options.ifMatch = open.ifMatch;
  }
  if (open.ifNoneMatch) {
    options.ifNoneMatch = open.ifNoneMatch;
  }
  return options;
}

function toProtoByteRange(range: ByteRange) {
  const proto: Record<string, unknown> = {};
  if (range.start !== undefined) {
    proto.start = normalizeProtoInt(range.start);
  }
  if (range.end !== undefined) {
    proto.end = normalizeProtoInt(range.end);
  }
  return proto;
}

function fromProtoByteRange(range: { start?: bigint; end?: bigint }): ByteRange {
  const value: ByteRange = {};
  if (range.start !== undefined) {
    value.start = range.start;
  }
  if (range.end !== undefined) {
    value.end = range.end;
  }
  return value;
}

function toProtoPresignMethod(method?: PresignMethod): ProtoPresignMethod {
  switch (method ?? PresignMethod.Get) {
    case PresignMethod.Get:
      return ProtoPresignMethod.GET;
    case PresignMethod.Put:
      return ProtoPresignMethod.PUT;
    case PresignMethod.Delete:
      return ProtoPresignMethod.DELETE;
    case PresignMethod.Head:
      return ProtoPresignMethod.HEAD;
  }
}

function fromProtoPresignMethod(method: ProtoPresignMethod): PresignMethod {
  switch (method) {
    case ProtoPresignMethod.PUT:
      return PresignMethod.Put;
    case ProtoPresignMethod.DELETE:
      return PresignMethod.Delete;
    case ProtoPresignMethod.HEAD:
      return PresignMethod.Head;
    case ProtoPresignMethod.GET:
    case ProtoPresignMethod.UNSPECIFIED:
    default:
      return PresignMethod.Get;
  }
}

function toProtoTimestamp(value: Date) {
  const millis = value.getTime();
  const seconds = Math.floor(millis / 1000);
  const nanos = Math.trunc((millis - (seconds * 1000)) * 1_000_000);
  return {
    seconds: BigInt(seconds),
    nanos,
  };
}

function fromProtoTimestamp(value: { seconds?: bigint; nanos?: number }): Date {
  const seconds = Number(value.seconds ?? 0n);
  const nanos = Number(value.nanos ?? 0);
  return new Date((seconds * 1000) + Math.trunc(nanos / 1_000_000));
}

function normalizeProtoInt(value: number | bigint | undefined): bigint {
  if (typeof value === "bigint") {
    return value;
  }
  if (value === undefined || !Number.isFinite(value)) {
    return 0n;
  }
  return BigInt(Math.trunc(value));
}

function cloneStringMap(values: Record<string, string> | undefined): Record<string, string> {
  if (!values) {
    return {};
  }
  return { ...values };
}

function cloneBytes(value: Uint8Array): Uint8Array {
  return new Uint8Array(value);
}

function toS3ConnectError(error: unknown, label: string): ConnectError {
  if (error instanceof ConnectError) {
    return error;
  }
  if (error instanceof S3NotFoundError) {
    return new ConnectError(error.message, Code.NotFound);
  }
  if (error instanceof S3PreconditionFailedError) {
    return new ConnectError(error.message, Code.FailedPrecondition);
  }
  if (error instanceof S3InvalidRangeError) {
    return new ConnectError(error.message, Code.OutOfRange);
  }
  return new ConnectError(`${label}: ${errorMessage(error)}`, Code.Unknown);
}

function mapS3RpcError(error: unknown): Error {
  const code = typeof error === "object" && error !== null && "code" in error
    ? (error as { code?: Code }).code
    : undefined;
  if (code === Code.NotFound) {
    return new S3NotFoundError(messageFromError(error));
  }
  if (code === Code.FailedPrecondition) {
    return new S3PreconditionFailedError(messageFromError(error));
  }
  if (code === Code.OutOfRange) {
    return new S3InvalidRangeError(messageFromError(error));
  }
  if (error instanceof Error) {
    return error;
  }
  return new Error(messageFromError(error));
}

async function s3Rpc<T>(fn: () => Promise<T>): Promise<T> {
  try {
    return await fn();
  } catch (error) {
    throw mapS3RpcError(error);
  }
}

function messageFromError(error: unknown): string {
  if (error instanceof Error && error.message) {
    return error.message;
  }
  return errorMessage(error);
}
