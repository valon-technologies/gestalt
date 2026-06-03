import {
  PresignMethod,
  S3NotFoundError,
  S3PreconditionFailedError,
  defineS3Provider,
} from "../../../src/index.ts";

const objects = new Map<string, {
  body: Uint8Array;
  contentType: string;
  metadata: Record<string, string>;
  lastModified: Date;
}>();

export const provider = defineS3Provider({
  description: "S3 fixture used by SDK tests",
  async headObject(ref) {
    const stored = objects.get(ref.key);
    if (!stored) {
      throw new S3NotFoundError();
    }
    return {
      ref,
      etag: `${stored.body.byteLength}`,
      size: BigInt(stored.body.byteLength),
      contentType: stored.contentType,
      lastModified: stored.lastModified,
      metadata: { ...stored.metadata },
      storageClass: "",
    };
  },
  async readObject(ref) {
    const stored = objects.get(ref.key);
    if (!stored) {
      throw new S3NotFoundError();
    }
    return {
      meta: await this.headObject(ref),
      body: stored.body,
    };
  },
  async writeObject(ref, body, options) {
    const chunks: Uint8Array[] = [];
    let total = 0;
    for await (const chunk of body) {
      chunks.push(chunk);
      total += chunk.byteLength;
    }
    const merged = new Uint8Array(total);
    let offset = 0;
    for (const chunk of chunks) {
      merged.set(chunk, offset);
      offset += chunk.byteLength;
    }
    const key = ref.key;
    if (options?.ifNoneMatch === "*" && objects.has(key)) {
      throw new S3PreconditionFailedError();
    }
    const lastModified = new Date();
    objects.set(key, {
      body: merged,
      contentType: options?.contentType ?? "",
      metadata: { ...(options?.metadata ?? {}) },
      lastModified,
    });
    return {
      ref,
      etag: `${merged.byteLength}`,
      size: BigInt(merged.byteLength),
      contentType: options?.contentType ?? "",
      lastModified,
      metadata: { ...(options?.metadata ?? {}) },
      storageClass: "",
    };
  },
  async deleteObject(ref) {
    objects.delete(ref.key);
  },
  async listObjects(options) {
    const matchingObjects = [...objects.keys()]
      .filter((key) => key.startsWith(options.prefix ?? ""))
      .sort();
    const listed = await Promise.all(matchingObjects.map((key) =>
      this.headObject({
        key,
      })
    ));
    return {
      objects: listed,
      commonPrefixes: [],
      nextContinuationToken: "",
      hasMore: false,
    };
  },
  async copyObject(source, destination) {
    const stored = objects.get(source.key);
    if (!stored) {
      throw new S3NotFoundError();
    }
    objects.set(destination.key, {
      body: new Uint8Array(stored.body),
      contentType: stored.contentType,
      metadata: { ...stored.metadata },
      lastModified: new Date(),
    });
    return await this.headObject(destination);
  },
  async presignObject(ref, options) {
    return {
      url: `https://fixture.invalid/${encodeURIComponent(ref.key)}?method=${options?.method ?? PresignMethod.Get}`,
      method: options?.method ?? PresignMethod.Get,
      headers: { ...(options?.headers ?? {}) },
      expiresAt: new Date(Date.now() + 60_000),
    };
  },
});
