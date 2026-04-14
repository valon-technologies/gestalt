import { Code, ConnectError } from "@connectrpc/connect";

import {
  defineFileAPIProvider,
  type CreateBlobOptions,
  type CreateFileOptions,
  type FileAPIBlobDescriptor,
  type FileAPIFileDescriptor,
  type FileAPIObjectDescriptor,
  type FileAPIProviderPart,
} from "../src/fileapi.ts";

const NATIVE_LINE_ENDING = process.platform === "win32" ? "\r\n" : "\n";

type StoredObject = Blob | File;

export function createMemoryFileAPIProvider(options: {
  displayName?: string;
  description?: string;
} = {}) {
  const objects = new Map<string, StoredObject>();
  const objectURLs = new Map<string, string>();
  let nextObjectId = 1;
  let nextObjectURL = 1;

  const put = (object: StoredObject): string => {
    const id = `obj-${nextObjectId++}`;
    objects.set(id, object);
    return id;
  };

  const get = (id: string): StoredObject => {
    const object = objects.get(id);
    if (!object) {
      throw new ConnectError(`unknown file object ${id}`, Code.NotFound);
    }
    return object;
  };

  const normalizeParts = (
    parts: FileAPIProviderPart[],
    lineEndings: CreateBlobOptions["endings"] | CreateFileOptions["endings"],
  ): globalThis.BlobPart[] =>
    parts.map((part) => {
      switch (part.kind) {
        case "string":
          return normalizeLineEndings(part.value, lineEndings);
        case "bytes":
          return cloneBytes(part.value);
        case "blob":
          return get(part.id);
      }
    });

  return defineFileAPIProvider({
    displayName: options.displayName ?? "Memory FileAPI",
    description: options.description ?? "In-memory FileAPI provider for tests",
    async createBlob(parts, blobOptions) {
      const blob = new Blob(normalizeParts(parts, blobOptions.endings), {
        type: blobOptions.mimeType ?? "",
      });
      return describeBlob(put(blob), blob);
    },
    async createFile(fileBits, fileName, fileOptions) {
      const file = new File(normalizeParts(fileBits, fileOptions.endings), fileName, {
        type: fileOptions.mimeType ?? "",
        lastModified: fileOptions.lastModified ?? Date.now(),
      });
      return describeFile(put(file), file);
    },
    async stat(id) {
      return describeObject(id, get(id));
    },
    async slice(request) {
      const object = get(request.id);
      const sliced = object.slice(request.start, request.end, request.contentType);
      return describeBlob(put(sliced), sliced);
    },
    async readBytes(id) {
      return new Uint8Array(await get(id).arrayBuffer());
    },
    async openReadStream(id) {
      const object = get(id);
      return streamObject(object);
    },
    async createObjectURL(id) {
      get(id);
      const url = `blob:gestalt:${nextObjectURL++}`;
      objectURLs.set(url, id);
      return url;
    },
    async resolveObjectURL(url) {
      const id = objectURLs.get(url);
      if (!id) {
        throw new ConnectError(`unknown object URL ${url}`, Code.NotFound);
      }
      return describeObject(id, get(id));
    },
    async revokeObjectURL(url) {
      objectURLs.delete(url);
    },
  });
}

function normalizeLineEndings(value: string, mode: CreateBlobOptions["endings"]): string {
  if (mode !== "native") {
    return value;
  }
  return value.replace(/\r\n|\r|\n/g, NATIVE_LINE_ENDING);
}

function cloneBytes(value: Uint8Array): Uint8Array<ArrayBuffer> {
  const copy = new Uint8Array(new ArrayBuffer(value.byteLength));
  copy.set(value);
  return copy;
}

function describeObject(id: string, object: StoredObject): FileAPIObjectDescriptor {
  if (object instanceof File) {
    return describeFile(id, object);
  }
  return describeBlob(id, object);
}

function describeBlob(id: string, blob: Blob): FileAPIBlobDescriptor {
  return {
    kind: "blob",
    id,
    size: blob.size,
    type: blob.type,
  };
}

function describeFile(id: string, file: File): FileAPIFileDescriptor {
  return {
    kind: "file",
    id,
    size: file.size,
    type: file.type,
    name: file.name,
    lastModified: file.lastModified,
  };
}

async function* streamObject(object: Blob): AsyncIterable<Uint8Array> {
  const reader = object.stream().getReader();
  try {
    while (true) {
      const next = await reader.read();
      if (next.done) {
        return;
      }
      yield next.value;
    }
  } finally {
    reader.releaseLock();
  }
}
