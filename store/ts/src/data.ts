import {type Bytes, type Name, type Store, StoreError, collect, concat, hex, nameOf, parseName, unhex} from "./store.js";

/** Exact binary keys; an empty path selects the current node. */
export type Key = Bytes;
export type TreePath = readonly Key[];
export type NodeChild<T> = readonly [Key, DeixisNode<T>];
export type Parts<T> = Readonly<{own: T; children: readonly NodeChild<T>[]}>;
/** A finite, acyclic tree with a mandatory own value and complete child map. */
export interface DeixisNode<T> {
  own(): T;
  children(): readonly NodeChild<T>[];
  at(path: TreePath): DeixisNode<T> | undefined;
  decompose(): Parts<T>;
}
/** Addressless access to fixed bytes. Successful reads return the same content. */
export interface Data {
  read(): Promise<Bytes>;
}
export type DataTree = DeixisNode<Data>;
export type Child = NodeChild<Data>;

/** Copies both input and output so materialized content remains immutable. */
export function bytesData(bytes: Bytes = new Uint8Array()): Data {
  const snapshot = new Uint8Array(bytes);
  return Object.freeze({async read(): Promise<Bytes> { return new Uint8Array(snapshot); }});
}
export function compare(a: Bytes, b: Bytes): number {
  for (let i=0; i<Math.min(a.length,b.length); i++) { const n=a[i]!-b[i]!; if (n) return Math.sign(n); }
  return Math.sign(a.length-b.length);
}
/** Copies children and keys in key order, refusing non-byte keys, missing children and duplicate keys. */
function sortedChildren<T>(children: Iterable<NodeChild<T>>): NodeChild<T>[] {
  const result=[...children].map(([k,d]) => {
    if(!(k instanceof Uint8Array)) throw new StoreError("invalid_key");
    if(d===undefined || d===null) throw new StoreError("invalid_child");
    return [new Uint8Array(k),d] as const;
  }).sort(([a],[b]) => compare(a,b));
  for (let i=1;i<result.length;i++) if (compare(result[i-1]![0],result[i]![0])===0) throw new StoreError("duplicate_key");
  return result;
}
/**
 * Walks children implemented outside this module through their complete
 * children() graph; sharing is allowed, a cycle is not. Nodes this module
 * constructed were validated when they were. own(), at() and readers are never called.
 */
function validateForeign<T>(children: readonly NodeChild<T>[]): void {
  const active=new Set<DeixisNode<T>>(),done=new Set<DeixisNode<T>>();
  const stack:{tree:DeixisNode<T>;exit:boolean}[]=children.map(([,tree])=>({tree,exit:false}));
  while(stack.length) {
    const {tree,exit}=stack.pop()!;
    if(tree instanceof Node) continue;
    if(exit) { active.delete(tree); done.add(tree); continue; }
    if(active.has(tree)) throw new StoreError("cyclic_tree");
    if(done.has(tree)) continue;
    active.add(tree); stack.push({tree,exit:true});
    for(const [,child] of sortedChildren(tree.children())) stack.push({tree:child,exit:false});
  }
}
class Node<T> implements DeixisNode<T> {
  readonly #own: T;
  readonly #children: NodeChild<T>[];
  constructor(own: T, children: Iterable<NodeChild<T>>) {
    this.#own = own;
    this.#children = sortedChildren(children);
    validateForeign(this.#children);
  }
  own(): T { return this.#own; }
  children(): readonly NodeChild<T>[] { return this.#children.map(([k,d]) => [new Uint8Array(k),d] as const); }
  at(path: TreePath): DeixisNode<T> | undefined { return select(this,path); }
  decompose(): Parts<T> { return {own:this.own(),children:this.children()}; }
}
export function compose<T>(own: T, children: Iterable<NodeChild<T>> = []): DeixisNode<T> { return new Node(own,children); }
export function select<T>(tree: DeixisNode<T>, path: TreePath): DeixisNode<T> | undefined {
  let current=tree;
  for(const key of path) {
    const child=current.children().find(([k])=>compare(k,key)===0);
    if(!child) return undefined;
    current=child[1];
  }
  return current;
}
export function dataTree(own: Data, children: Iterable<Child> = []): DataTree {
  if(own===undefined || own===null || typeof own.read!=="function") throw new StoreError("invalid_data");
  return compose(own,children);
}
/** Derived read: select(tree,path).own().read(); a missing node is not empty data. */
export async function read(tree: DataTree, path: TreePath = [], signal?: AbortSignal): Promise<Bytes> {
  signal?.throwIfAborted();
  const node=select(tree,path);
  if(!node) throw new StoreError("path_not_found");
  const bytes=await node.own().read();
  signal?.throwIfAborted();
  return new Uint8Array(bytes);
}
export const dataProfile = "deixis-codec-v2/identity-bytes@v0.4.0";
export type Root = Readonly<{profile: string; address: string}>;
const rootOf = (n: Name): Root => Object.freeze({profile:dataProfile,address:`dxl2:${n.slice(7)}`});
function rootName(root: Root): Name {
  if (root.profile!==dataProfile) throw new CodecError("unsupported","unsupported_profile");
  if (!/^dxl2:[0-9a-f]{64}$/.test(root.address)) throw new StoreError("invalid_name");
  return parseName(`sha256:${root.address.slice(5)}`);
}
export class CodecError extends Error {
  constructor(readonly classification: "invalid"|"unsupported"|"resource-refused", readonly code: string, readonly dimension?: string) {
    super(`${classification}: ${code}${dimension ? ` (${dimension})` : ""}`); this.name="CodecError";
  }
}
const invalid = (code:string): never => { throw new CodecError("invalid",code); };
const limit = (dimension:string): never => { throw new CodecError("resource-refused","limit_exceeded",dimension); };
export interface DataTreeLimits {
  keyBytes?:number; payloadBytes?:number; children?:number; chunkBytes?:number; flatBytes?:number;
  depth?:number; nodes?:number; uniqueChunks?:number; uniqueBytes?:number; unfoldedBytes?:number;
}
type Limits = Required<DataTreeLimits>;
function defaults(l: DataTreeLimits): Limits {
  const result = {keyBytes:4096,payloadBytes:16<<20,children:65536,chunkBytes:32<<20,flatBytes:64<<20,depth:256,nodes:16777216,uniqueChunks:1000000,uniqueBytes:2**30,unfoldedBytes:2**30,...l};
  for (const v of Object.values(result)) if (!Number.isSafeInteger(v) || v<1) throw new RangeError("DataTree limits must be positive safe integers");
  return result;
}
function number(n: number): Bytes {
  const result:number[]=[]; do { const b=n%128; n=Math.floor(n/128); result.push(b+(n?128:0)); } while(n); return Uint8Array.from(result);
}
const field = (b: Bytes): Bytes => concat([number(b.length),b]);
const header = (magic:string): Bytes => Uint8Array.from([...magic].map(c=>c.charCodeAt(0)).concat([2,0,1]));
const identity = Uint8Array.of(0,1);

class Parser {
  i=0;
  constructor(readonly b: Bytes, readonly l: Limits) {}
  take(n: number): Bytes { if (n>this.b.length-this.i) invalid("unexpected_eof"); const b=this.b.subarray(this.i,this.i+n); this.i+=n; return b; }
  num(): bigint {
    let n=0n;
    for (let i=0;i<10;i++) {
      if (this.i===this.b.length) invalid(i===0?"unexpected_eof":"malformed_uvarint");
      const b=this.b[this.i++]!;
      if (i===9 && (b&128)) invalid("malformed_uvarint");
      if (i===9 && b>1) invalid("uvarint_overflow");
      n |= BigInt(b&127)<<BigInt(7*i);
      if (!(b&128)) { if (i>0 && b===0) invalid("non_shortest_uvarint"); return n; }
    }
    return invalid("malformed_uvarint");
  }
  bounded(max:number,dimension:string):number { const n=this.num(); if(n>BigInt(max)) limit(dimension); return Number(n); }
  field(max:number,dimension:string):Bytes { return this.take(this.bounded(max,dimension)); }
  header(magic:string):Bytes {
    if (String.fromCharCode(...this.take(4))!==magic) invalid("unknown_magic");
    const n=this.num(); if(n<2n || n>32n) invalid("malformed_slot_codec_id");
    const id=this.take(Number(n)); checkID(id,this.l); return id;
  }
  key(previous:Bytes|undefined):Bytes {
    const k=this.field(this.l.keyBytes,"key_length");
    if (previous) { const n=compare(k,previous); if(n===0) invalid("duplicate_key"); if(n<0) invalid("unsorted_keys"); }
    return k;
  }
  end():void { if(this.i!==this.b.length) invalid("trailing_bytes"); }
}
function checkID(id:Bytes,l:Limits):void {
  const p=new Parser(id,l);
  for (;;) {
    if(p.i===id.length) invalid("malformed_slot_codec_id");
    const kind=id[p.i++]!; if(kind===2) continue; if(kind>2) return;
    if(kind===1) { if(id.length-p.i<16) invalid("malformed_slot_codec_id"); p.take(16); }
    if(p.i===id.length) invalid("malformed_slot_codec_id");
    const n=p.num(); if((kind===0 && n===0n)||p.i!==id.length) invalid("malformed_slot_codec_id"); return;
  }
}
function supported(id:Bytes):void { if(compare(id,identity)!==0) throw new CodecError("unsupported","unsupported_slot_codec"); }
function canonicalChildren(data:DataTree,l:Limits):Child[] {
  const input=data.children();
  if(input.length>l.children) limit("entries_per_node");
  for(const [key] of input) if(key.length>l.keyBytes) limit("key_length");
  const cs=input.map(([key,child])=>[new Uint8Array(key),child] as const).sort(([a],[b])=>compare(a,b));
  for(let i=1;i<cs.length;i++) if(compare(cs[i-1]![0],cs[i]![0])===0) invalid("duplicate_key");
  return cs;
}
function checkNode(own:Bytes,cs:readonly Child[],depth:number,l:Limits):void {
  if(depth>l.depth) limit("logical_depth"); if(own.length>l.payloadBytes) limit("payload_length");
  if(cs.length>l.children) limit("entries_per_node"); for(const [k] of cs) if(k.length>l.keyBytes) limit("key_length");
}
export async function encodeFlat(data:DataTree, options:DataTreeLimits={},signal?:AbortSignal):Promise<Bytes> {
  const l=defaults(options),parts:Bytes[]=[header("dxf2")],active=new Set<DataTree>(); let nodes=0,size=7;
  const add=(b:Bytes)=>{ size+=b.length; if(size>l.flatBytes) limit("flat_artifact_octets"); if(size>l.unfoldedBytes) limit("unfolded_flat_octets"); parts.push(b); };
  const visit=async(d:DataTree,depth:number):Promise<void>=>{
    signal?.throwIfAborted(); if(depth>l.depth) limit("logical_depth"); if(active.has(d)) invalid("cyclic_tree");
    if(++nodes>l.nodes) limit("unfolded_node_count"); active.add(d);
    const own=await read(d,[],signal),cs=canonicalChildren(d,l); checkNode(own,cs,depth,l);
    add(field(own)); add(number(cs.length)); for(const [k,c] of cs) { add(field(k)); await visit(c,depth+1); }
    active.delete(d);
  };
  await visit(data,0); return concat(parts);
}
export function decodeFlat(bytes:Bytes,options:DataTreeLimits={}):DataTree {
  const l=defaults(options); if(bytes.length>l.flatBytes) limit("flat_artifact_octets");
  const p=new Parser(bytes,l),id=p.header("dxf2"); let nodes=0;
  const visit=(depth:number):DataTree=>{
    if(depth>l.depth) limit("logical_depth"); if(++nodes>l.nodes) limit("unfolded_node_count");
    const own=p.field(l.payloadBytes,"payload_length"),n=p.bounded(l.children,"entries_per_node"),cs:Child[]=[]; let prev:Bytes|undefined;
    for(let i=0;i<n;i++) { const key=p.key(prev); prev=key; cs.push([key,visit(depth+1)]); } return dataTree(bytesData(own),cs);
  };
  const d=visit(0); p.end(); supported(id); return d;
}
type Chunk={own:Bytes; id:Bytes; children:readonly (readonly [Bytes,Name])[]};
function decodeChunk(bytes:Bytes,l:Limits,expected?:Bytes):Chunk {
  if(bytes.length>l.chunkBytes) limit("chunk_octets");
  const p=new Parser(bytes,l),id=p.header("dxl2"),n=p.bounded(l.children,"links_per_chunk"),names:Name[]=[],seen=new Set<Name>();
  for(let i=0;i<n;i++) { const name:Name=`sha256:${hex(p.take(32))}`; if(seen.has(name)) invalid("duplicate_link_hash"); seen.add(name); names.push(name); }
  const own=p.field(l.payloadBytes,"payload_length"),count=p.bounded(l.children,"entries_per_node"),cs:[Bytes,Name][]=[],used=new Set<number>();
  let next=0,prev:Bytes|undefined;
  for(let i=0;i<count;i++) {
    const key=p.key(prev); prev=key; const index=p.num(); if(index>=BigInt(n)) invalid("bad_link_index"); const j=Number(index);
    if(!used.has(j)) { if(j!==next) invalid("links_out_of_order"); used.add(j); next++; } cs.push([new Uint8Array(key),names[j]!]);
  }
  if(next!==n) invalid("unused_link"); p.end(); if(expected && compare(id,expected)!==0) invalid("slot_codec_mismatch"); supported(id);
  return {own:new Uint8Array(own),id:new Uint8Array(id),children:cs};
}
type Measure={nodes:number;octets:number;height:number};
const measure=(own:Bytes,count:number):Measure=>({nodes:1,octets:number(own.length).length+own.length+number(count).length,height:0});
function addMeasure(m:Measure,key:Bytes,c:Measure,l:Limits):void {
  if(c.nodes>l.nodes || m.nodes>l.nodes-c.nodes) limit("unfolded_node_count");
  const extra=number(key.length).length+key.length;
  if(c.octets>l.unfoldedBytes-extra || m.octets>l.unfoldedBytes-extra-c.octets) limit("unfolded_flat_octets");
  m.nodes+=c.nodes; m.octets+=extra+c.octets; m.height=Math.max(m.height,c.height+1); if(m.height>l.depth) limit("logical_depth");
}
function checkMeasure(m:Measure,l:Limits):void { if(m.nodes>l.nodes) limit("unfolded_node_count"); if(m.octets>l.unfoldedBytes-7) limit("unfolded_flat_octets"); }

/** Leaves precede parents in the returned Map's insertion order. */
export async function encodeLinked(data:DataTree,options:DataTreeLimits={},signal?:AbortSignal):Promise<{root:Root;chunks:Map<Name,Bytes>}> {
  const l=defaults(options),chunks=new Map<Name,Bytes>(),memo=new Map<DataTree,{name:Name;m:Measure}>(),active=new Set<DataTree>(); let total=0;
  const visit=async(d:DataTree,depth:number):Promise<{name:Name;m:Measure}>=>{
    signal?.throwIfAborted(); if(depth>l.depth) limit("logical_depth"); if(active.has(d)) invalid("cyclic_tree"); const old=memo.get(d);
    if(old) { if(old.m.height>l.depth-depth) limit("logical_depth"); return old; }
    active.add(d); const own=await read(d,[],signal),cs=canonicalChildren(d,l); checkNode(own,cs,depth,l);
    const m=measure(own,cs.length),indices=new Map<Name,number>(),names:Name[]=[],body:Bytes[]=[field(own),number(cs.length)];
    for(const [key,child] of cs) {
      const c=await visit(child,depth+1); addMeasure(m,key,c.m,l); let index=indices.get(c.name);
      if(index===undefined) { index=names.length; names.push(c.name); indices.set(c.name,index); } body.push(field(key),number(index));
    }
    checkMeasure(m,l); const bytes=concat([header("dxl2"),number(names.length),...names.map(n=>unhex(n.slice(7))),...body]);
    if(bytes.length>l.chunkBytes) limit("chunk_octets"); const name=await nameOf(bytes); signal?.throwIfAborted();
    if(!chunks.has(name)) {
      if(chunks.size>=l.uniqueChunks) limit("unique_chunks"); if(bytes.length>l.uniqueBytes-total) limit("unique_octets");
      total+=bytes.length; chunks.set(name,bytes);
    }
    const e={name,m}; memo.set(d,e); active.delete(d); return e;
  };
  const {name}=await visit(data,0); return {root:rootOf(name),chunks};
}
export async function putDataTree(store:Store,data:DataTree,options:DataTreeLimits={},signal?:AbortSignal):Promise<Root> {
  signal?.throwIfAborted(); const {root,chunks}=await encodeLinked(data,options,signal);
  for(const [name,b] of chunks) { signal?.throwIfAborted(); if(await store.put(b,signal)!==name) throw new StoreError("integrity","put acknowledged a different name"); }
  return root;
}
async function fetchChunk(store:Store,name:Name,l:Limits,signal?:AbortSignal,expected?:Bytes):Promise<{chunk:Chunk;size:number}> {
  signal?.throwIfAborted(); let b:Bytes;
  try { b=await collect(await store.getStream(name,signal),l.chunkBytes,signal); }
  catch(e) { if(e instanceof StoreError && e.code==="too_large") limit("chunk_octets"); throw e; }
  if(await nameOf(b)!==name) throw new StoreError("integrity"); signal?.throwIfAborted(); return {chunk:decodeChunk(b,l,expected),size:b.length};
}
/** A verified node; children are addresses and are fetched only when selected. */
export class DataTreeView {
  readonly #root:Root; readonly #store:Store; readonly #limits:Limits; readonly #chunk:Chunk;
  private constructor(root:Root,store:Store,l:Limits,c:Chunk) { this.#root=Object.freeze({...root}); this.#store=store; this.#limits=l; this.#chunk=c; }
  static async open(store:Store,root:Root,options:DataTreeLimits={},signal?:AbortSignal):Promise<DataTreeView> {
    const snapshot=Object.freeze({...root}),name=rootName(snapshot),l=defaults(options),{chunk}=await fetchChunk(store,name,l,signal); return new DataTreeView(snapshot,store,l,chunk);
  }
  root():Root { return this.#root; }
  own():Bytes { return new Uint8Array(this.#chunk.own); }
  children():readonly (readonly [Bytes,Root])[] { return this.#chunk.children.map(([k,n])=>[new Uint8Array(k),rootOf(n)]); }
  async at(path:readonly Bytes[],signal?:AbortSignal):Promise<DataTreeView|undefined> {
    signal?.throwIfAborted(); if(path.length>this.#limits.depth) limit("logical_depth"); let v:DataTreeView=this;
    for(const key of path) {
      const found=v.#chunk.children.find(([k])=>compare(k,key)===0); if(!found) return undefined;
      const {chunk}=await fetchChunk(v.#store,found[1],v.#limits,signal,v.#chunk.id); v=new DataTreeView(rootOf(found[1]),v.#store,v.#limits,chunk);
    } return v;
  }
}
export const openDataTree=DataTreeView.open;
export async function loadDataTree(store:Store,root:Root,options:DataTreeLimits={},signal?:AbortSignal):Promise<DataTree> {
  const name=rootName(root),l=defaults(options),memo=new Map<Name,{d:DataTree;m:Measure}|undefined>(); let total=0;
  const visit=async(name:Name,depth:number):Promise<{d:DataTree;m:Measure}>=>{
    signal?.throwIfAborted(); if(depth>l.depth) limit("logical_depth");
    if(memo.has(name)) { const old=memo.get(name); if(!old) return invalid("cyclic_link"); if(old.m.height>l.depth-depth) limit("logical_depth"); return old; }
    if(memo.size>=l.uniqueChunks) limit("unique_chunks");
    const {chunk:c,size}=await fetchChunk(store,name,l,signal,depth?identity:undefined);
    if(size>l.uniqueBytes-total) limit("unique_octets"); total+=size; memo.set(name,undefined);
    const cs:Child[]=[],m=measure(c.own,c.children.length);
    for(const [k,n] of c.children) { const e=await visit(n,depth+1); addMeasure(m,k,e.m,l); cs.push([k,e.d]); }
    checkMeasure(m,l); const e={d:dataTree(bytesData(c.own),cs),m}; memo.set(name,e); return e;
  };
  return (await visit(name,0)).d;
}
