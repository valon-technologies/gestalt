import type { JsonValue } from "./model.ts";

type JsonRecord = Record<string, JsonValue>;

const base64Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

export async function encodeStructuredClone(value: unknown): Promise<JsonValue> {
  const seen = new Map<object, number>();
  const nodes: Array<JsonValue | Promise<JsonValue>> = [];

  const atom = (input: unknown): JsonValue => {
    if (input === null || typeof input === "string" || typeof input === "boolean") return input;
    if (typeof input === "number") {
      return Number.isFinite(input) && !Object.is(input, -0)
        ? input
        : { $: "number", value: numberToken(input) };
    }
    if (typeof input === "undefined") return { $: "undefined" };
    if (typeof input === "bigint") return { $: "bigint", value: input.toString() };
    if (typeof input !== "object") throw dataCloneError();

    const existing = seen.get(input);
    if (existing !== undefined) return { $: "ref", index: existing };
    const index = nodes.length;
    seen.set(input, index);
    nodes.push(null);
    nodes[index] = encodeNode(input, atom);
    return { $: "ref", index };
  };

  const root = atom(value);
  const resolved: JsonValue[] = [];
  for (const node of nodes) resolved.push(await node);
  return { $structuredClone: 1, root, nodes: resolved };
}

export function decodeStructuredClone(wire: JsonValue): unknown {
  const envelope = record(wire, "structured-clone envelope");
  if (envelope.$structuredClone !== 1 || !Array.isArray(envelope.nodes)) {
    throw new WireCodecError("invalid structured-clone envelope");
  }
  const nodeWires = envelope.nodes;
  const values = new Array<unknown>(nodeWires.length);

  for (let index = 0; index < nodeWires.length; index += 1) {
    const node = record(nodeWires[index]!, `node ${index}`);
    const type = string(node.type, "node type");
    switch (type) {
      case "array": values[index] = []; break;
      case "object": values[index] = node.nullPrototype === true ? Object.create(null) : {}; break;
      case "map": values[index] = new Map(); break;
      case "set": values[index] = new Set(); break;
      case "date": values[index] = new Date(numberFromTokenOrValue(node.value)); break;
      case "regexp": values[index] = new RegExp(string(node.source, "regexp source"), string(node.flags, "regexp flags")); break;
      case "error": values[index] = createError(node); break;
      case "domexception": values[index] = new DOMException(
        string(node.message, "DOMException message"),
        string(node.name, "DOMException name"),
      ); break;
      case "arraybuffer": values[index] = bytesFromBase64(string(node.bytes, "ArrayBuffer bytes")).buffer; break;
      case "blob": values[index] = new Blob(
        [ownedBuffer(bytesFromBase64(string(node.bytes, "Blob bytes")))],
        { type: string(node.mime, "Blob type") },
      ); break;
      case "file": values[index] = createFile(node); break;
      case "imagedata": values[index] = createImageData(node); break;
      case "boxed": values[index] = createBoxed(node); break;
      case "typed": break;
      default: throw new WireCodecError(`unsupported structured-clone node ${type}`);
    }
  }

  const decodeAtom = (input: JsonValue): unknown => {
    if (input === null || typeof input === "string" || typeof input === "boolean" || typeof input === "number") {
      return input;
    }
    const tagged = record(input, "structured-clone atom");
    switch (tagged.$) {
      case "undefined": return undefined;
      case "number": return numberFromToken(string(tagged.value, "number token"));
      case "bigint": return BigInt(string(tagged.value, "bigint value"));
      case "ref": {
        const index = integer(tagged.index, "reference index");
        if (index < 0 || index >= values.length) throw new WireCodecError("reference is out of range");
        return values[index];
      }
      case "hole": return hole;
      default: throw new WireCodecError("invalid structured-clone atom");
    }
  };

  for (let index = 0; index < nodeWires.length; index += 1) {
    const node = record(nodeWires[index]!, `node ${index}`);
    if (node.type !== "typed") continue;
    const buffer = decodeAtom(node.buffer as JsonValue);
    if (!(buffer instanceof ArrayBuffer)) throw new WireCodecError("typed-array buffer is not an ArrayBuffer");
    values[index] = createView(
      string(node.name, "view name"),
      buffer,
      integer(node.byteOffset, "view byteOffset"),
      integer(node.length, "view length"),
    );
  }

  for (let index = 0; index < nodeWires.length; index += 1) {
    const node = record(nodeWires[index]!, `node ${index}`);
    const target = values[index];
    switch (node.type) {
      case "array": {
        if (!Array.isArray(node.items) || !Array.isArray(target)) throw new WireCodecError("invalid array node");
        target.length = node.items.length;
        node.items.forEach((item, itemIndex) => {
          const decoded = decodeAtom(item);
          if (decoded !== hole) target[itemIndex] = decoded;
        });
        defineEntries(target, node.entries, decodeAtom);
        break;
      }
      case "object": defineEntries(target as object, node.entries, decodeAtom); break;
      case "map": {
        if (!(target instanceof Map) || !Array.isArray(node.entries)) throw new WireCodecError("invalid map node");
        for (const entry of node.entries) {
          if (!Array.isArray(entry) || entry.length !== 2) throw new WireCodecError("invalid map entry");
          target.set(decodeAtom(entry[0]!), decodeAtom(entry[1]!));
        }
        break;
      }
      case "set": {
        if (!(target instanceof Set) || !Array.isArray(node.values)) throw new WireCodecError("invalid set node");
        for (const entry of node.values) target.add(decodeAtom(entry));
        break;
      }
      case "error": {
        if (target instanceof Error && node.cause !== undefined) {
          Object.defineProperty(target, "cause", { value: decodeAtom(node.cause), configurable: true, writable: true });
        }
        break;
      }
    }
  }

  return decodeAtom(envelope.root as JsonValue);
}

export function encodeIDBKey(value: unknown): JsonValue {
  const seen = new Set<object>();
  return encodeKey(value, seen);
}

export function decodeIDBKey(value: JsonValue): IDBValidKey {
  const encoded = record(value, "IndexedDB key");
  switch (encoded.type) {
    case "number": return numberFromTokenOrValue(encoded.value);
    case "date": return new Date(number(encoded.value, "date key"));
    case "string": return string(encoded.value, "string key");
    case "binary": return ownedBuffer(bytesFromBase64(string(encoded.value, "binary key")));
    case "array": {
      if (!Array.isArray(encoded.value)) throw new WireCodecError("invalid array key");
      return encoded.value.map(decodeIDBKey);
    }
    default: throw new WireCodecError("invalid IndexedDB key type");
  }
}

export function cloneIDBKey<T extends IDBValidKey>(value: T): T {
  return decodeIDBKey(encodeIDBKey(value)) as T;
}

export function compareIDBKeys(left: unknown, right: unknown): number {
  const a = decodeIDBKey(encodeIDBKey(left));
  const b = decodeIDBKey(encodeIDBKey(right));
  const rankA = keyRank(a);
  const rankB = keyRank(b);
  if (rankA !== rankB) return rankA < rankB ? -1 : 1;
  if (typeof a === "number" && typeof b === "number") return a < b ? -1 : a > b ? 1 : 0;
  if (a instanceof Date && b instanceof Date) return Math.sign(a.valueOf() - b.valueOf());
  if (typeof a === "string" && typeof b === "string") return a < b ? -1 : a > b ? 1 : 0;
  if (isBufferSource(a) && isBufferSource(b)) return compareBytes(bytesOf(a), bytesOf(b));
  const arrayA = a as IDBValidKey[];
  const arrayB = b as IDBValidKey[];
  for (let index = 0; index < Math.min(arrayA.length, arrayB.length); index += 1) {
    const compared = compareIDBKeys(arrayA[index], arrayB[index]);
    if (compared !== 0) return compared;
  }
  return Math.sign(arrayA.length - arrayB.length);
}

export function assertIDBKey(value: unknown): asserts value is IDBValidKey {
  encodeIDBKey(value);
}

async function encodeNode(value: object, atom: (value: unknown) => JsonValue): Promise<JsonValue> {
  if (Array.isArray(value)) {
    const items: JsonValue[] = [];
    for (let index = 0; index < value.length; index += 1) {
      items.push(Object.hasOwn(value, index) ? atom(value[index]) : { $: "hole" });
    }
    const entries = Object.keys(value)
      .filter((key) => !isArrayIndex(key, value.length))
      .map((key) => [key, atom((value as unknown as Record<string, unknown>)[key])] as JsonValue);
    return { type: "array", items, entries };
  }
  if (value instanceof Date) {
    const timestamp = value.valueOf();
    return { type: "date", value: Number.isFinite(timestamp) ? timestamp : numberToken(timestamp) };
  }
  if (value instanceof RegExp) return { type: "regexp", source: value.source, flags: value.flags };
  if (value instanceof Map) {
    return { type: "map", entries: [...value].map(([key, entry]) => [atom(key), atom(entry)]) };
  }
  if (value instanceof Set) return { type: "set", values: [...value].map(atom) };
  if (isFile(value)) {
    return {
      type: "file",
      name: value.name,
      mime: value.type,
      lastModified: value.lastModified,
      bytes: base64FromBytes(new Uint8Array(await value.arrayBuffer())),
    };
  }
  if (value instanceof Blob) {
    return {
      type: "blob",
      mime: value.type,
      bytes: base64FromBytes(new Uint8Array(await value.arrayBuffer())),
    };
  }
  if (value instanceof ArrayBuffer) return { type: "arraybuffer", bytes: base64FromBytes(new Uint8Array(value)) };
  if (ArrayBuffer.isView(value)) {
    return {
      type: "typed",
      name: viewName(value),
      buffer: atom(value.buffer),
      byteOffset: value.byteOffset,
      length: value instanceof DataView
        ? value.byteLength
        : (value as ArrayBufferView & { length: number }).length,
    };
  }
  if (isImageData(value)) {
    return {
      type: "imagedata",
      width: value.width,
      height: value.height,
      colorSpace: value.colorSpace,
      bytes: base64FromBytes(value.data),
    };
  }
  if (value instanceof DOMException) {
    return { type: "domexception", name: value.name, message: value.message };
  }
  if (value instanceof Error) {
    const error: JsonRecord = {
      type: "error",
      name: value.name,
      message: value.message,
      ...(value.stack === undefined ? {} : { stack: value.stack }),
    };
    if ("cause" in value) error.cause = atom(value.cause);
    return error;
  }
  if (isBoxedPrimitive(value)) return encodeBoxed(value);

  const prototype = Object.getPrototypeOf(value);
  if (prototype !== Object.prototype && prototype !== null) throw dataCloneError();
  return {
    type: "object",
    nullPrototype: prototype === null,
    entries: Object.keys(value).map((key) => [key, atom((value as Record<string, unknown>)[key])]),
  };
}

function encodeKey(value: unknown, seen: Set<object>): JsonValue {
  if (typeof value === "number") {
    if (Number.isNaN(value)) throw dataError();
    return { type: "number", value: Number.isFinite(value) && !Object.is(value, -0) ? value : numberToken(value) };
  }
  if (value instanceof Date) {
    if (Number.isNaN(value.valueOf())) throw dataError();
    return { type: "date", value: value.valueOf() };
  }
  if (typeof value === "string") return { type: "string", value };
  if (isBufferSource(value)) return { type: "binary", value: base64FromBytes(bytesOf(value)) };
  if (Array.isArray(value)) {
    if (seen.has(value)) throw dataError();
    seen.add(value);
    const encoded: JsonValue[] = [];
    for (let index = 0; index < value.length; index += 1) {
      if (!Object.hasOwn(value, index)) throw dataError();
      encoded.push(encodeKey(value[index], seen));
    }
    return { type: "array", value: encoded };
  }
  throw dataError();
}

function createError(node: JsonRecord): Error {
  const message = string(node.message, "error message");
  const name = string(node.name, "error name");
  const constructors: Record<string, ErrorConstructor> = {
    Error, EvalError, RangeError, ReferenceError, SyntaxError, TypeError, URIError,
  };
  const error = new (constructors[name] ?? Error)(message);
  error.name = name;
  if (typeof node.stack === "string") error.stack = node.stack;
  return error;
}

function createFile(node: JsonRecord): Blob {
  const bytes = bytesFromBase64(string(node.bytes, "File bytes"));
  const options = {
    type: string(node.mime, "File type"),
    lastModified: number(node.lastModified, "File lastModified"),
  };
  const FileConstructor = (globalThis as { File?: typeof File }).File;
  const buffer = ownedBuffer(bytes);
  if (FileConstructor) return new FileConstructor([buffer], string(node.name, "File name"), options);
  const blob = new Blob([buffer], options);
  Object.defineProperties(blob, {
    name: { value: string(node.name, "File name"), enumerable: true },
    lastModified: { value: options.lastModified, enumerable: true },
  });
  return blob;
}

function createImageData(node: JsonRecord): unknown {
  const data = new Uint8ClampedArray(bytesFromBase64(string(node.bytes, "ImageData bytes")));
  const ImageDataConstructor = (globalThis as { ImageData?: typeof ImageData }).ImageData;
  if (!ImageDataConstructor) {
    return {
      data,
      width: integer(node.width, "ImageData width"),
      height: integer(node.height, "ImageData height"),
      colorSpace: string(node.colorSpace, "ImageData colorSpace"),
    };
  }
  return new ImageDataConstructor(
    data,
    integer(node.width, "ImageData width"),
    integer(node.height, "ImageData height"),
    { colorSpace: string(node.colorSpace, "ImageData colorSpace") as PredefinedColorSpace },
  );
}

function createBoxed(node: JsonRecord): object {
  switch (node.name) {
    case "Boolean": return new Boolean(node.value);
    case "Number": return new Number(
      typeof node.value === "number" ? node.value : numberFromToken(string(node.value, "boxed number")),
    );
    case "String": return new String(string(node.value, "boxed string"));
    case "BigInt": return Object(BigInt(string(node.value, "boxed bigint")));
    default: throw new WireCodecError("invalid boxed primitive");
  }
}

function encodeBoxed(value: Boolean | Number | String | object): JsonValue {
  const name = Object.prototype.toString.call(value).slice(8, -1);
  const primitive = (value as { valueOf(): unknown }).valueOf();
  if (typeof primitive === "number") {
    return { type: "boxed", name, value: Number.isFinite(primitive) && !Object.is(primitive, -0) ? primitive : numberToken(primitive) };
  }
  if (typeof primitive === "bigint") return { type: "boxed", name, value: primitive.toString() };
  if (typeof primitive === "string" || typeof primitive === "boolean") return { type: "boxed", name, value: primitive };
  throw dataCloneError();
}

function createView(name: string, buffer: ArrayBuffer, byteOffset: number, length: number): ArrayBufferView {
  if (name === "DataView") return new DataView(buffer, byteOffset, length);
  const constructors: Record<string, new (buffer: ArrayBuffer, byteOffset: number, length: number) => ArrayBufferView> = {
    Int8Array, Uint8Array, Uint8ClampedArray, Int16Array, Uint16Array, Int32Array, Uint32Array,
    Float32Array, Float64Array, BigInt64Array, BigUint64Array,
  };
  const Constructor = constructors[name];
  if (!Constructor) throw new WireCodecError(`unsupported typed array ${name}`);
  return new Constructor(buffer, byteOffset, length);
}

function viewName(value: ArrayBufferView): string {
  return Object.prototype.toString.call(value).slice(8, -1);
}

function defineEntries(
  target: object,
  wire: JsonValue | undefined,
  decode: (value: JsonValue) => unknown,
): void {
  if (!Array.isArray(wire)) throw new WireCodecError("invalid object entries");
  for (const entry of wire) {
    if (!Array.isArray(entry) || entry.length !== 2 || typeof entry[0] !== "string") {
      throw new WireCodecError("invalid object entry");
    }
    Object.defineProperty(target, entry[0], {
      value: decode(entry[1]!),
      enumerable: true,
      configurable: true,
      writable: true,
    });
  }
}

function isArrayIndex(value: string, length: number): boolean {
  const number = Number(value);
  return Number.isInteger(number) && number >= 0 && number < length && String(number) === value;
}

function isFile(value: object): value is File {
  const Constructor = (globalThis as { File?: typeof File }).File;
  return (
    (typeof Constructor === "function" && value instanceof Constructor) ||
    (
      value instanceof Blob &&
      typeof (value as Blob & { name?: unknown }).name === "string" &&
      typeof (value as Blob & { lastModified?: unknown }).lastModified === "number"
    )
  );
}

function isImageData(value: object): value is ImageData {
  const Constructor = (globalThis as { ImageData?: typeof ImageData }).ImageData;
  return typeof Constructor === "function" && value instanceof Constructor;
}

function isBoxedPrimitive(value: object): value is Boolean | Number | String {
  return ["[object Boolean]", "[object Number]", "[object String]", "[object BigInt]"].includes(
    Object.prototype.toString.call(value),
  );
}

function isBufferSource(value: unknown): value is BufferSource {
  return value instanceof ArrayBuffer || ArrayBuffer.isView(value);
}

function bytesOf(value: BufferSource): Uint8Array<ArrayBuffer> {
  return value instanceof ArrayBuffer
    ? new Uint8Array(value.slice(0) as ArrayBuffer)
    : new Uint8Array(value.buffer.slice(value.byteOffset, value.byteOffset + value.byteLength) as ArrayBuffer);
}

function compareBytes(left: Uint8Array, right: Uint8Array): number {
  for (let index = 0; index < Math.min(left.length, right.length); index += 1) {
    if (left[index] !== right[index]) return left[index]! < right[index]! ? -1 : 1;
  }
  return Math.sign(left.length - right.length);
}

function keyRank(value: IDBValidKey): number {
  if (typeof value === "number") return 1;
  if (value instanceof Date) return 2;
  if (typeof value === "string") return 3;
  if (isBufferSource(value)) return 4;
  return 5;
}

function numberToken(value: number): string {
  if (Number.isNaN(value)) return "nan";
  if (value === Infinity) return "+infinity";
  if (value === -Infinity) return "-infinity";
  if (Object.is(value, -0)) return "-0";
  throw new WireCodecError("finite number does not need a token");
}

function numberFromToken(value: string): number {
  switch (value) {
    case "nan": return NaN;
    case "+infinity": return Infinity;
    case "-infinity": return -Infinity;
    case "-0": return -0;
    default: throw new WireCodecError("invalid number token");
  }
}

function numberFromTokenOrValue(value: JsonValue | undefined): number {
  if (typeof value === "number") return value;
  return numberFromToken(string(value, "number key"));
}

function base64FromBytes(bytes: ArrayLike<number>): string {
  let result = "";
  for (let index = 0; index < bytes.length; index += 3) {
    const first = bytes[index]!;
    const second = bytes[index + 1];
    const third = bytes[index + 2];
    result += base64Alphabet[first >> 2];
    result += base64Alphabet[((first & 3) << 4) | ((second ?? 0) >> 4)];
    result += second === undefined ? "=" : base64Alphabet[((second & 15) << 2) | ((third ?? 0) >> 6)];
    result += third === undefined ? "=" : base64Alphabet[third & 63];
  }
  return result;
}

function bytesFromBase64(value: string): Uint8Array<ArrayBuffer> {
  if (value.length % 4 !== 0 || !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(value)) {
    throw new WireCodecError("invalid base64 data");
  }
  const bytes: number[] = [];
  for (let index = 0; index < value.length; index += 4) {
    const a = base64Alphabet.indexOf(value[index]!);
    const b = base64Alphabet.indexOf(value[index + 1]!);
    const c = value[index + 2] === "=" ? 0 : base64Alphabet.indexOf(value[index + 2]!);
    const d = value[index + 3] === "=" ? 0 : base64Alphabet.indexOf(value[index + 3]!);
    bytes.push((a << 2) | (b >> 4));
    if (value[index + 2] !== "=") bytes.push(((b & 15) << 4) | (c >> 2));
    if (value[index + 3] !== "=") bytes.push(((c & 3) << 6) | d);
  }
  const result = new Uint8Array(new ArrayBuffer(bytes.length));
  result.set(bytes);
  return result;
}

function ownedBuffer(bytes: Uint8Array): ArrayBuffer {
  return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) as ArrayBuffer;
}

function record(value: JsonValue | undefined, label: string): JsonRecord {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new WireCodecError(`${label} must be an object`);
  }
  return value;
}

function string(value: JsonValue | undefined, label: string): string {
  if (typeof value !== "string") throw new WireCodecError(`${label} must be a string`);
  return value;
}

function number(value: JsonValue | undefined, label: string): number {
  if (typeof value !== "number" || !Number.isFinite(value)) throw new WireCodecError(`${label} must be a number`);
  return value;
}

function integer(value: JsonValue | undefined, label: string): number {
  const result = number(value, label);
  if (!Number.isInteger(result)) throw new WireCodecError(`${label} must be an integer`);
  return result;
}

function dataCloneError(): DOMException {
  return new DOMException("The value cannot be serialized for storage", "DataCloneError");
}

function dataError(): DOMException {
  return new DOMException("The value is not a valid IndexedDB key", "DataError");
}

const hole = Symbol("structured-clone-hole");

export class WireCodecError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "WireCodecError";
  }
}
