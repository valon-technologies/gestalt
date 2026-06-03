import { defineCacheProvider } from "../../../src/index.ts";

type CacheRecord = {
  value: Uint8Array;
  ttlMs: number;
};

const store = new Map<string, CacheRecord>();

let configuredPrefix = "";

function scopedKey(key: string): string {
  return configuredPrefix ? `${configuredPrefix}:${key}` : key;
}

function cloneBytes(value: Uint8Array): Uint8Array {
  return new Uint8Array(value);
}

export const provider = defineCacheProvider({
  displayName: "Fixture Cache",
  description: "Cache fixture used by SDK tests",
  configure(_name, config) {
    configuredPrefix = String(config.prefix ?? "");
  },
  async get(key) {
    return store.get(scopedKey(key))?.value;
  },
  async getMany(keys) {
    const entries = Object.create(null) as Record<string, Uint8Array>;
    for (const key of keys) {
      const record = store.get(scopedKey(key));
      if (!record) {
        continue;
      }
      entries[key] = cloneBytes(record.value);
    }
    return entries;
  },
  async set(key, value, options) {
    store.set(scopedKey(key), {
      value: cloneBytes(value),
      ttlMs: Math.max(0, Math.trunc(options?.ttlMs ?? 0)),
    });
  },
  async setMany(entries, options) {
    for (const entry of entries) {
      store.set(scopedKey(entry.key), {
        value: cloneBytes(entry.value),
        ttlMs: Math.max(0, Math.trunc(options?.ttlMs ?? 0)),
      });
    }
  },
  async delete(key) {
    return store.delete(scopedKey(key));
  },
  async deleteMany(keys) {
    let deleted = 0;
    for (const key of keys) {
      if (store.delete(scopedKey(key))) {
        deleted += 1;
      }
    }
    return deleted;
  },
  async touch(key, ttlMs) {
    const existing = store.get(scopedKey(key));
    if (!existing) {
      return false;
    }
    store.set(scopedKey(key), {
      value: cloneBytes(existing.value),
      ttlMs: Math.max(0, Math.trunc(ttlMs)),
    });
    return true;
  },
});
