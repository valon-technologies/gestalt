export const provider = {
  kind: "fileapi",
  resolveName() {},
  configureProvider() {},
  runtimeMetadata() {
    return { kind: "fileapi" };
  },
  supportsHealthCheck() {
    return false;
  },
  healthCheck() {},
  warnings() {
    return [];
  },
  closeProvider() {},
  createBlob() {
    throw new Error("not implemented");
  },
  stat() {
    throw new Error("not implemented");
  },
  slice() {
    throw new Error("not implemented");
  },
  readBytes() {
    throw new Error("not implemented");
  },
  openReadStream() {
    return [];
  },
  createObjectURL() {
    throw new Error("not implemented");
  },
  resolveObjectURL() {
    throw new Error("not implemented");
  },
  revokeObjectURL() {
    throw new Error("not implemented");
  },
};
