import { create } from "@bufbuild/protobuf";
import { EmptySchema } from "@bufbuild/protobuf/wkt";
import {
  Code,
  ConnectError,
  type ServiceImpl,
} from "@connectrpc/connect";

import {
  CopyObjectResponseSchema,
  HeadObjectResponseSchema,
  ListObjectsResponseSchema,
  PresignMethod as ProtoPresignMethod,
  PresignObjectResponseSchema,
  ReadObjectChunkSchema,
  S3 as S3Service,
  type ByteRange as ProtoByteRange,
  type ReadObjectRequest as ProtoReadObjectRequest,
  type S3ObjectRef as ProtoS3ObjectRef,
  WriteObjectResponseSchema,
} from "../internal/gen/v1/s3_pb.ts";
import { PresignMethod, type ByteRange } from "../s3.ts";
import { errorMessage, type MaybePromise } from "../api.ts";
import { ProviderBase, type ProviderBaseOptions } from "../provider.ts";
import { dateFromTimestamp, timestampFromDate } from "../protocol.ts";

const WRITE_CHUNK_SIZE = 64 * 1024;
const textEncoder = new TextEncoder();

/**
 * Error returned when an object reference does not exist.
 */
export class S3NotFoundError extends Error {
  constructor(message?: string) {
    super(message ?? "s3: not found");
    this.name = "S3NotFoundError";
  }
}

/**
 * Error returned when conditional read or write preconditions fail.
 */
export class S3PreconditionFailedError extends Error {
  constructor(message?: string) {
    super(message ?? "s3: precondition failed");
    this.name = "S3PreconditionFailedError";
  }
}

/**
 * Error returned when the requested byte range is invalid.
 */
export class S3InvalidRangeError extends Error {
  constructor(message?: string) {
    super(message ?? "s3: invalid range");
    this.name = "S3InvalidRangeError";
  }
}

/**
 * Identifies a concrete object or object version.
 */
export interface ObjectRef {
  key: string;
  versionId?: string;
}

/**
 * Metadata returned for an S3 object.
 */
export interface ObjectMeta {
  ref: ObjectRef;
  etag: string;
  size: bigint;
  contentType: string;
  lastModified?: Date;
  metadata: Record<string, string>;
  storageClass: string;
}

/**
 * Conditional and range options for reads.
 */
export interface ReadOptions {
  range?: ByteRange;
  ifMatch?: string;
  ifNoneMatch?: string;
  ifModifiedSince?: Date;
  ifUnmodifiedSince?: Date;
}

/**
 * Optional headers and conditions for writes.
 */
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

/**
 * Listing options for object pagination and prefix filtering.
 */
export interface ListOptions {
  prefix?: string;
  delimiter?: string;
  continuationToken?: string;
  startAfter?: string;
  maxKeys?: number;
}

/**
 * Single page of results returned by {@link S3.listObjects}.
 */
export interface ListPage {
  objects: ObjectMeta[];
  commonPrefixes: string[];
  nextContinuationToken: string;
  hasMore: boolean;
}

/**
 * Conditional options for server-side copy operations.
 */
export interface CopyOptions {
  ifMatch?: string;
  ifNoneMatch?: string;
}

/**
 * Options used when generating a presigned URL.
 */
export interface PresignOptions {
  method?: PresignMethod;
  expiresSeconds?: number | bigint;
  contentType?: string;
  contentDisposition?: string;
  headers?: Record<string, string>;
}

/**
 * Result returned by an authored {@link S3ProviderOptions.presignObject} handler.
 */
export interface PresignResult {
  url: string;
  method: PresignMethod;
  expiresAt?: Date;
  headers: Record<string, string>;
}

/**
 * Accepted write body sources for authored S3 provider read results.
 */
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

/**
 * Result returned by an authored S3 provider implementation.
 */
export interface ProviderReadResult {
  meta: ObjectMeta;
  body?: S3BodySource;
}

/**
 * Runtime hooks required to implement a Gestalt S3 provider.
 */
export interface S3ProviderOptions extends ProviderBaseOptions {
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

/**
 * S3 provider implementation consumed by the Gestalt runtime.
 */
export class S3Provider extends ProviderBase {
  readonly kind = "s3" as const;

  private readonly headObjectHandler: S3ProviderOptions["headObject"];
  private readonly readObjectHandler: S3ProviderOptions["readObject"];
  private readonly writeObjectHandler: S3ProviderOptions["writeObject"];
  private readonly deleteObjectHandler: S3ProviderOptions["deleteObject"];
  private readonly listObjectsHandler: S3ProviderOptions["listObjects"];
  private readonly copyObjectHandler: S3ProviderOptions["copyObject"];
  private readonly presignObjectHandler: S3ProviderOptions["presignObject"];

  constructor(options: S3ProviderOptions) {
    super(options, "s3");
    this.headObjectHandler = options.headObject;
    this.readObjectHandler = options.readObject;
    this.writeObjectHandler = options.writeObject;
    this.deleteObjectHandler = options.deleteObject;
    this.listObjectsHandler = options.listObjects;
    this.copyObjectHandler = options.copyObject;
    this.presignObjectHandler = options.presignObject;
  }

  /** Fetches object metadata without reading the object body. */
  async headObject(ref: ObjectRef): Promise<ObjectMeta> {
    return await this.headObjectHandler(ref);
  }

  /** Reads an object body from the provider implementation. */
  async readObject(
    ref: ObjectRef,
    options?: ReadOptions,
  ): Promise<ProviderReadResult> {
    return await this.readObjectHandler(ref, options);
  }

  /** Writes an object body through the provider implementation. */
  async writeObject(
    ref: ObjectRef,
    body: AsyncIterable<Uint8Array>,
    options?: WriteOptions,
  ): Promise<ObjectMeta> {
    return await this.writeObjectHandler(ref, body, options);
  }

  /** Deletes an object from the provider implementation. */
  async deleteObject(ref: ObjectRef): Promise<void> {
    await this.deleteObjectHandler(ref);
  }

  /** Lists objects from the provider implementation. */
  async listObjects(options: ListOptions): Promise<ListPage> {
    return await this.listObjectsHandler(options);
  }

  /** Copies an object in the provider implementation. */
  async copyObject(
    source: ObjectRef,
    destination: ObjectRef,
    options?: CopyOptions,
  ): Promise<ObjectMeta> {
    return await this.copyObjectHandler(source, destination, options);
  }

  /** Generates a presigned URL from the provider implementation. */
  async presignObject(
    ref: ObjectRef,
    options?: PresignOptions,
  ): Promise<PresignResult> {
    return await this.presignObjectHandler(ref, options);
  }
}

/**
 * Creates an S3 provider from standard object storage handlers.
 */
export function defineS3Provider(options: S3ProviderOptions): S3Provider {
  return new S3Provider(options);
}

/**
 * Runtime type guard for S3 providers loaded from user modules.
 */
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

/**
 * Adapts an authored S3 provider to the shared protocol service implementation.
 *
 * @internal
 */
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
      const options: ListOptions = {};
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
        method: request.method === ProtoPresignMethod.UNSPECIFIED
          ? PresignMethod.GET
          : request.method,
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
        method: (result.method === PresignMethod.UNSPECIFIED
          ? ProtoPresignMethod.GET
          : result.method) as ProtoPresignMethod,
        headers: cloneStringMap(result.headers),
      } as {
        url: string;
        method: ProtoPresignMethod;
        headers: Record<string, string>;
        expiresAt?: { seconds: bigint; nanos: number };
      };
      if (result.expiresAt) {
        response.expiresAt = timestampFromDate(result.expiresAt);
      }
      return create(PresignObjectResponseSchema, response);
    },
  };
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
async function* toAsyncByteStream(body?: S3BodySource): AsyncIterable<Uint8Array> {
  if (body == null) {
    return;
  }
  if (typeof body === "string") {
    yield* chunkBytes(textEncoder.encode(body));
    return;
  }
  const bytes = asS3ByteArray(body);
  if (bytes) {
    yield* chunkBytes(bytes);
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

function asS3ByteArray(body?: S3BodySource): Uint8Array | undefined {
  if (body instanceof Uint8Array) {
    return body;
  }
  if (body instanceof ArrayBuffer) {
    return new Uint8Array(body);
  }
  if (ArrayBuffer.isView(body)) {
    return new Uint8Array(body.buffer, body.byteOffset, body.byteLength);
  }
  return undefined;
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
    key: ref.key,
    versionId: ref.versionId ?? "",
  };
}

function fromProtoObjectRef(ref: ProtoS3ObjectRef | undefined): ObjectRef {
  const value: ObjectRef = {
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
    value.lastModified = timestampFromDate(meta.lastModified);
  }
  return value;
}
function fromProtoReadOptions(request: ProtoReadObjectRequest | undefined): ReadOptions {
  const options: ReadOptions = {};
  if (request?.range) {
    options.range = fromProtoByteRange(request.range);
  }
  if (request?.ifMatch) {
    options.ifMatch = request.ifMatch;
  }
  if (request?.ifNoneMatch) {
    options.ifNoneMatch = request.ifNoneMatch;
  }
  if (request?.ifModifiedSince) {
    options.ifModifiedSince = dateFromTimestamp(request.ifModifiedSince);
  }
  if (request?.ifUnmodifiedSince) {
    options.ifUnmodifiedSince = dateFromTimestamp(request.ifUnmodifiedSince);
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
function fromProtoByteRange(range: ProtoByteRange | undefined): ByteRange {
  const value: ByteRange = {};
  if (range?.start !== undefined) {
    value.start = range.start;
  }
  if (range?.end !== undefined) {
    value.end = range.end;
  }
  return value;
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
