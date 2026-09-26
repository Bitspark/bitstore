/** Raw bytes; keys are not text and undergo no Unicode normalization. */
export type Bytes = Uint8Array;
export type Name = `sha256:${string}`;
export const contract = "2026-09-24";

export class StoreError extends Error {
  constructor(readonly code: string, message = code) { super(message); this.name = "StoreError"; }
}
export function parseName(value: string): Name {
  if (!/^sha256:[0-9a-f]{64}$/.test(value)) throw new StoreError("invalid_name");
  return value as Name;
}
export function hex(bytes: Bytes): string { return Array.from(bytes, b => b.toString(16).padStart(2, "0")).join(""); }
export function unhex(value: string): Bytes { return Uint8Array.from(value.match(/../g) ?? [], b => parseInt(b, 16)); }
export async function nameOf(bytes: Bytes): Promise<Name> {
  return `sha256:${hex(new Uint8Array(await crypto.subtle.digest("SHA-256", new Uint8Array(bytes))))}`;
}
export type PutResult = { name: Name; size: number } | { error: Error };

/** Nine raw blob operations. Plural absence is omission; singular absence throws
 * not_found. A streamed upload commits only after clean EOF. */
export interface Store {
  get(name: Name, signal?: AbortSignal): Promise<Bytes>;
  getMany(names: readonly Name[], signal?: AbortSignal): Promise<Map<Name, Bytes>>;
  has(names: readonly Name[], signal?: AbortSignal): Promise<Name[]>;
  size(names: readonly Name[], signal?: AbortSignal): Promise<Map<Name, number>>;
  getRange(name: Name, offset: number, length: number, signal?: AbortSignal): Promise<Bytes>;
  getStream(name: Name, signal?: AbortSignal): Promise<ReadableStream<Bytes>>;
  put(bytes: Bytes, signal?: AbortSignal): Promise<Name>;
  putMany(items: readonly Bytes[], signal?: AbortSignal): Promise<PutResult[]>;
  putStream(stream: ReadableStream<Bytes>, signal?: AbortSignal): Promise<{name: Name; size: number}>;
}

/** Bounded collection cancels the underlying stream on refusal or abort. */
export async function collect(stream: ReadableStream<Bytes>, max: number, signal?: AbortSignal): Promise<Bytes> {
  signal?.throwIfAborted();
  const reader = stream.getReader(); const parts: Bytes[] = []; let size = 0;
  const abort = () => { void reader.cancel(signal?.reason).catch(() => {}); };
  signal?.addEventListener("abort", abort, {once: true});
  try {
    for (;;) {
      const {done, value} = await reader.read(); signal?.throwIfAborted(); if (done) break;
      size += value.byteLength;
      if (size > max) throw new StoreError("too_large");
      parts.push(new Uint8Array(value));
    }
    return concat(parts, size);
  } catch (e) { await reader.cancel(e).catch(() => {}); throw e; }
  finally { signal?.removeEventListener("abort", abort); reader.releaseLock(); }
}
export function concat(parts: readonly Bytes[], size = parts.reduce((n,b) => n+b.length, 0)): Bytes {
  const out = new Uint8Array(size); let i = 0; for (const part of parts) { out.set(part,i); i += part.length; } return out;
}
export function streamOf(bytes: Bytes): ReadableStream<Bytes> {
  return new ReadableStream({start(c) { c.enqueue(new Uint8Array(bytes)); c.close(); }});
}

/** An in-process Store, with no durability promise. Input and output are copied. */
export class MemoryStore implements Store {
  #blobs = new Map<Name, Bytes>();
  constructor(readonly maxBytes = 0) { if (!Number.isSafeInteger(maxBytes) || maxBytes < 0) throw new RangeError("maxBytes"); }
  async get(name: Name, signal?: AbortSignal): Promise<Bytes> {
    signal?.throwIfAborted(); parseName(name); const b = this.#blobs.get(name);
    if (!b) throw new StoreError("not_found"); return new Uint8Array(b);
  }
  async getMany(names: readonly Name[], signal?: AbortSignal): Promise<Map<Name,Bytes>> {
    signal?.throwIfAborted(); const result = new Map<Name,Bytes>();
    for (const n of names) { parseName(n); const b = this.#blobs.get(n); if (b) result.set(n,new Uint8Array(b)); } return result;
  }
  async size(names: readonly Name[], signal?: AbortSignal): Promise<Map<Name,number>> {
    signal?.throwIfAborted(); const result = new Map<Name,number>();
    for (const n of names) { parseName(n); const b = this.#blobs.get(n); if (b) result.set(n,b.length); } return result;
  }
  async has(names: readonly Name[], signal?: AbortSignal): Promise<Name[]> { return [...(await this.size(names,signal)).keys()]; }
  async getRange(name: Name, offset: number, length: number, signal?: AbortSignal): Promise<Bytes> {
    if (!Number.isSafeInteger(offset) || offset < 0 || !Number.isSafeInteger(length)) throw new StoreError("bad_request");
    const b = await this.get(name,signal); return b.slice(offset,length < 0 ? undefined : Math.min(b.length,offset+length));
  }
  async getStream(name: Name, signal?: AbortSignal): Promise<ReadableStream<Bytes>> { return streamOf(await this.get(name,signal)); }
  async put(bytes: Bytes, signal?: AbortSignal): Promise<Name> {
    signal?.throwIfAborted(); const copy = new Uint8Array(bytes);
    if (this.maxBytes && copy.length > this.maxBytes) throw new StoreError("too_large");
    const n = await nameOf(copy); signal?.throwIfAborted(); this.#blobs.set(n,copy); return n;
  }
  async putMany(items: readonly Bytes[], signal?: AbortSignal): Promise<PutResult[]> {
    const result: PutResult[] = []; for (const b of items) {
      try { result.push({name: await this.put(b,signal),size:b.length}); }
      catch (error) { result.push({error: error instanceof Error ? error : new Error(String(error))}); }
    } return result;
  }
  async putStream(stream: ReadableStream<Bytes>, signal?: AbortSignal): Promise<{name:Name;size:number}> {
    const b = await collect(stream,this.maxBytes || Number.MAX_SAFE_INTEGER,signal);
    return {name:await this.put(b,signal),size:b.length};
  }
}
