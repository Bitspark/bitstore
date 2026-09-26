import {type Bytes, type Name, type Store, type PutResult, StoreError, collect, concat, contract, nameOf, parseName, streamOf} from "./store.js";

export const bytesProfile = Object.freeze({id:"bitstore/bytes",version:"1",binding:"bitstore-http/2026-09-24"});
export interface ClientOptions { fetch?: typeof fetch; maxBytes?: number }
function protocol(message:string):never { throw new StoreError("protocol",message); }
const sizeValue = (v:unknown):v is number => Number.isSafeInteger(v) && (v as number)>=0;
const utf8 = new TextEncoder();

/** Operations HTTP Store. Full reads and upload acknowledgements are verified.
 * getStream verifies at EOF: consume through EOF to establish integrity.
 * WebCrypto hashing buffers at most maxBytes (64 MiB by default). Streamed
 * uploads are collected before sending, so input failure commits nothing.
 * These are client resource policies, not changes to the raw Store contract. */
export class Client implements Store {
  readonly #base:string; readonly #fetch:typeof fetch; readonly #max:number;
  servedContract:string|undefined;
  constructor(endpoint:string,options:ClientOptions={}) {
    const u=new URL(endpoint);
    if(!["http:","https:"].includes(u.protocol)||u.username||u.password||u.search||u.hash) throw new StoreError("bad_request","expected an HTTP endpoint without credentials, query or fragment");
    this.#base=u.href.replace(/\/$/,"");this.#fetch=options.fetch??fetch;this.#max=options.maxBytes??64*1024*1024;
    if(!Number.isSafeInteger(this.#max)||this.#max<1)throw new RangeError("maxBytes");
  }
  async #request(path:string,init:RequestInit,signal?:AbortSignal):Promise<Response> {
    signal?.throwIfAborted();const r=await this.#fetch(this.#base+path,{...init,signal,redirect:"error"});
    if(r.headers.get("Bit-Service")!=="bitstore"||![null,"operations"].includes(r.headers.get("Bit-Surface"))||!r.headers.get("Bit-Contract")) {
      await r.body?.cancel();protocol("wrong service or surface, or missing contract");
    }
    this.servedContract=r.headers.get("Bit-Contract")!;
    if(!r.ok) {
      let e:{error?:unknown;message?:unknown};try { e=await this.#json(r,signal); } catch { protocol("invalid refusal envelope"); }
      if(typeof e!.error!=="string"||typeof e!.message!=="string"||!e!.message)protocol("invalid refusal envelope");
      const expected:Record<string,number>={not_found:404,too_large:413,bad_request:400};
      if(!(e!.error in expected)||r.status!==expected[e!.error])protocol("unknown refusal or wrong status");
      throw new StoreError(e!.error,e!.message);
    }
    if(r.status!==200) {await r.body?.cancel();protocol("unexpected success status");} return r;
  }
  async #json(r:Response,signal?:AbortSignal):Promise<any> {
    if(r.headers.get("Content-Type")?.split(";")[0]?.trim()!=="application/json") {await r.body?.cancel();protocol("expected JSON");}
    const bytes=await collect(r.body??streamOf(new Uint8Array()),4*1024*1024,signal);
    try{return JSON.parse(new TextDecoder("utf-8",{fatal:true}).decode(bytes));}catch{ return protocol("invalid JSON"); }
  }
  #names(names:readonly Name[]):string {for(const n of names)parseName(n);return JSON.stringify({names});}
  async describe(signal?:AbortSignal):Promise<any> {return this.#json(await this.#request("/describe",{},signal),signal);}
  async getStream(name:Name,signal?:AbortSignal):Promise<ReadableStream<Bytes>> {
    parseName(name);const r=await this.#request("/v1/blobs/"+encodeURIComponent(name),{},signal);
    if(r.headers.get("Content-Type")?.split(";")[0]?.trim()!=="application/octet-stream") {await r.body?.cancel();protocol("expected raw bytes");}
    const reader=(r.body??streamOf(new Uint8Array())).getReader(),parts:Bytes[]=[];let size=0,finished=false;const max=this.#max;
    const cleanup=()=>signal?.removeEventListener("abort",abort);
    const abort=()=>{void reader.cancel(signal?.reason).catch(()=>{});};signal?.addEventListener("abort",abort,{once:true});
    return new ReadableStream<Bytes>({
      async pull(c){
        try {
          signal?.throwIfAborted();const {done,value}=await reader.read();signal?.throwIfAborted();
          if(done){finished=true;cleanup();reader.releaseLock();if(await nameOf(concat(parts,size))!==name)throw new StoreError("integrity");c.close();return;}
          size+=value.length;if(size>max)throw new StoreError("too_large");parts.push(new Uint8Array(value));c.enqueue(new Uint8Array(value));
        }catch(e){cleanup();if(!finished){await reader.cancel(e).catch(()=>{});reader.releaseLock();}c.error(e);}
      },
      async cancel(reason){cleanup();if(!finished){finished=true;await reader.cancel(reason);reader.releaseLock();}}
    });
  }
  async get(name:Name,signal?:AbortSignal):Promise<Bytes>{return collect(await this.getStream(name,signal),this.#max,signal);}
  async size(names:readonly Name[],signal?:AbortSignal):Promise<Map<Name,number>> {
    const r=await this.#request("/v1/stat",{method:"POST",headers:{"Content-Type":"application/json"},body:this.#names(names)},signal),j=await this.#json(r,signal);
    if(!j||typeof j.sizes!=="object"||!j.sizes||Array.isArray(j.sizes))protocol("invalid sizes");
    const asked=new Set(names),result=new Map<Name,number>();for(const [n,v]of Object.entries(j.sizes)){if(!asked.has(n as Name)||!sizeValue(v))protocol("unasked name or invalid size");result.set(n as Name,v);}return result;
  }
  async has(names:readonly Name[],signal?:AbortSignal):Promise<Name[]>{const sizes=await this.size(names,signal);return [...new Set(names)].filter(n=>sizes.has(n));}
  async getRange(name:Name,offset:number,length:number,signal?:AbortSignal):Promise<Bytes>{
    parseName(name);if(!Number.isSafeInteger(offset)||offset<0||!Number.isSafeInteger(length))throw new StoreError("bad_request");
    const r=await this.#request(`/v1/blobs/${encodeURIComponent(name)}?offset=${offset}&length=${length}`,{},signal);
    if(r.headers.get("Content-Type")?.split(";")[0]?.trim()!=="application/octet-stream"){await r.body?.cancel();protocol("expected raw range");}
    const b=await collect(r.body??streamOf(new Uint8Array()),this.#max,signal);if(length>=0&&b.length>length)protocol("oversized range");return b;
  }
  async put(bytes:Bytes,signal?:AbortSignal):Promise<Name>{
    const b=new Uint8Array(bytes);if(b.length>this.#max)throw new StoreError("too_large");const expected=await nameOf(b);
    const r=await this.#request("/v1/blobs",{method:"POST",headers:{"Content-Type":"application/octet-stream"},body:b},signal),j=await this.#json(r,signal);
    if(!j||j.name!==expected)throw new StoreError("integrity");if(j.size!==b.length)protocol("wrong upload size");return expected;
  }
  async putStream(stream:ReadableStream<Bytes>,signal?:AbortSignal):Promise<{name:Name;size:number}>{const b=await collect(stream,this.#max,signal);return {name:await this.put(b,signal),size:b.length};}
  async getMany(names:readonly Name[],signal?:AbortSignal):Promise<Map<Name,Bytes>> {
    const r=await this.#request("/v1/get",{method:"POST",headers:{"Content-Type":"application/json"},body:this.#names(names)},signal);
    if(r.headers.get("Content-Type")?.split(";")[0]?.trim()!=="application/x-bitstore-blobs"){await r.body?.cancel();protocol("expected blob batch");}
    const b=await collect(r.body??streamOf(new Uint8Array()),this.#max,signal),asked=new Set(names),result=new Map<Name,Bytes>();let i=0;
    while(i<b.length){
      const end=b.indexOf(10,i);if(end<0||end-i>128)protocol("invalid batch header");
      const h=new TextDecoder().decode(b.subarray(i,end)),match=/^(sha256:[0-9a-f]{64}) (0|[1-9][0-9]*)$/.exec(h);
      if(!match)protocol("invalid batch header");const n=match[1] as Name,size=Number(match[2]);
      if(!sizeValue(size)||size>b.length-end-1||!asked.has(n)||result.has(n))protocol("invalid batch record");
      const item=b.slice(end+1,end+1+size);if(await nameOf(item)!==n)throw new StoreError("integrity");result.set(n,item);i=end+1+size;
    }return result;
  }
  async putMany(items:readonly Bytes[],signal?:AbortSignal):Promise<PutResult[]>{
    const parts:Bytes[]=[],expected:Name[]=[],sizes:number[]=[];let total=0;
    for(const item of items){const b=new Uint8Array(item),h=utf8.encode(`${b.length}\n`);total+=h.length+b.length;if(total>this.#max)throw new StoreError("too_large");parts.push(h,b);sizes.push(b.length);expected.push(await nameOf(b));}
    const r=await this.#request("/v1/put",{method:"POST",headers:{"Content-Type":"application/x-bitstore-blobs"},body:new Uint8Array(concat(parts))},signal),j=await this.#json(r,signal);
    if(!j||!Array.isArray(j.results)||j.results.length!==items.length)protocol("wrong batch result count");
    return j.results.map((item:any,i:number):PutResult=>{
      if(item&&item.error!==undefined){if(item.name!==undefined||item.size!==undefined||item.error!=="too_large"||typeof item.message!=="string"||!item.message)protocol("invalid item refusal");return {error:new StoreError(item.error,item.message)};}
      if(!item||item.name!==expected[i])throw new StoreError("integrity");if(item.size!==sizes[i])protocol("wrong item size");return {name:expected[i]!,size:sizes[i]!};
    });
  }
}
/** Explicit adapter discovery. Unknown/duplicate versions fail before data I/O. */
export async function discoverBytes(endpoint:string,options:ClientOptions={},signal?:AbortSignal):Promise<Client>{
  const client=new Client(endpoint,options),d=await client.describe(signal),profiles=Array.isArray(d?.profiles)?d.profiles.filter((p:any)=>p?.id===bytesProfile.id):[];
  if(profiles.length!==1||profiles[0].version!==bytesProfile.version||profiles[0].binding!==bytesProfile.binding||d.service!=="bitstore"||![undefined,"operations"].includes(d.surface)||d.contract!==contract||client.servedContract!==contract||d.bindings?.primary!=="http"||d.bindings?.http?.prefix!=="/v1")protocol("incompatible bytes profile or describe envelope");
  return client;
}
