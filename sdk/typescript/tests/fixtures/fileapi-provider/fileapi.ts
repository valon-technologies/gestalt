import { create } from "@bufbuild/protobuf";

import { defineFileAPIProvider } from "../../../src/index.ts";
import {
  BytesResponseSchema,
  FileObjectKind,
  FileObjectResponseSchema,
  ObjectURLResponseSchema,
  ReadChunkSchema,
} from "../../../gen/v1/fileapi_pb.ts";

export const provider = defineFileAPIProvider({
  displayName: "Fixture FileAPI",
  description: "FileAPI fixture used by SDK tests",
  createBlob(request) {
    return create(FileObjectResponseSchema, {
      object: {
        kind: FileObjectKind.BLOB,
        id: `blob:${request.parts.length}`,
        size: BigInt(request.parts.length),
        mimeType: request.options?.mimeType ?? "application/octet-stream",
        name: "",
      },
    });
  },
  createFile(request) {
    return create(FileObjectResponseSchema, {
      object: {
        kind: FileObjectKind.FILE,
        id: `file:${request.name}`,
        size: BigInt(request.parts.length),
        mimeType: request.options?.mimeType ?? "text/plain",
        name: request.name,
      },
    });
  },
  stat(request) {
    return create(FileObjectResponseSchema, {
      object: {
        kind: FileObjectKind.FILE,
        id: request.id,
        size: 7n,
        mimeType: "text/plain",
        name: "fixture.txt",
      },
    });
  },
  slice(request) {
    return create(FileObjectResponseSchema, {
      object: {
        kind: FileObjectKind.BLOB,
        id: `${request.id}:slice`,
        size: 2n,
        mimeType: request.mimeType || "application/octet-stream",
        name: "",
      },
    });
  },
  readBytes(request) {
    return create(BytesResponseSchema, {
      data: new TextEncoder().encode(`bytes:${request.id}`),
    });
  },
  openReadStream(request) {
    return [
      create(ReadChunkSchema, {
        data: new TextEncoder().encode(`${request.id}:chunk-1`),
      }),
      create(ReadChunkSchema, {
        data: new TextEncoder().encode(`${request.id}:chunk-2`),
      }),
    ];
  },
  createObjectURL(request) {
    return create(ObjectURLResponseSchema, {
      url: `memory://${request.id}`,
    });
  },
  resolveObjectURL(request) {
    return create(FileObjectResponseSchema, {
      object: {
        kind: FileObjectKind.FILE,
        id: request.url.replace("memory://", ""),
        size: 9n,
        mimeType: "text/plain",
        name: "resolved.txt",
      },
    });
  },
  revokeObjectURL(_request) {},
});
