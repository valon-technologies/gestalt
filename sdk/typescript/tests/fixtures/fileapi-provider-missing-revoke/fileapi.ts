export const provider = {
  kind: "fileapi",
  createBlob() {
    throw new Error("not implemented");
  },
  createFile() {
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
};
