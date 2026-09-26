import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {bytesData, dataTree, compose, select, read, MemoryStore, StoreError, CodecError, nameOf, encodeFlat, decodeFlat, encodeLinked, putDataTree, openDataTree, loadDataTree, dataProfile} from '../dist/index.js';
const file=JSON.parse(readFileSync(new URL('../../../vectors/data.json',import.meta.url)));
const b=s=>Uint8Array.from(Buffer.from(s,'hex'));
const hx=b=>Buffer.from(b).toString('hex');
const isCode=code=>e=>e.code===code;
const tree=(own=new Uint8Array(),children=[])=>dataTree(bytesData(own),children);
for(const v of file.vectors) test(v.label,async()=>{
  const d=decodeFlat(b(v.flat)); assert.equal(hx(await encodeFlat(d)),v.flat);
  const {root,chunks}=await encodeLinked(d);assert.deepEqual(root,v.root);assert.deepEqual(Object.fromEntries([...chunks].map(([n,b])=>[n,hx(b)])),v.chunks);
  const store=new MemoryStore();for(const [n,hex] of Object.entries(v.chunks)) assert.equal(await store.put(b(hex)),n);
  assert.equal(hx(await encodeFlat(await loadDataTree(store,v.root))),v.flat);
});
for(const v of file.invalidFlat) test(v.label,()=>assert.throws(()=>decodeFlat(b(v.hex)),isCode(v.code)));
test('exact keys, immutable own and children, reconstruction',async()=>{
  const own=b('ff'),key=b('ff'),leaf=tree();const d=tree(own,[[key,leaf],[b(''),leaf]]);own[0]=0;key[0]=0;(await d.own().read())[0]=1;d.children()[1][0][0]=1;
  assert.equal(hx(await d.own().read()),'ff');assert.equal(d.at([b('ff')]),leaf);assert.equal(d.at([]),d);assert.equal(d.at([b('')]),leaf);assert.equal(d.at([b('00')]),undefined);
  const parts=d.decompose();assert.deepEqual(await encodeFlat(compose(parts.own,parts.children)),await encodeFlat(d));assert.throws(()=>tree(b(''),[[b(''),leaf],[b(''),leaf]]));
});
test('Data owns only read; DataTree derives read through shared structure',async()=>{
  const seen=[];
  const rootData={async read(){seen.push('root');return b('72')}};
  const childData={async read(){seen.push('child');return b('ff')}};
  const child=dataTree(childData),root=dataTree(rootData,[[b('00'),child]]);
  assert.equal(root.own(),rootData);assert.equal(select(root,[]),root);
  assert.equal(select(root,[b('00')]),child);
  assert.equal(hx(await read(root,[b('00')])),'ff');assert.deepEqual(seen,['child']);
  await assert.rejects(read(root,[b('01')]),isCode('path_not_found'));assert.deepEqual(seen,['child']);
  assert.equal(hx(await read(root)),'72');
  const parts=root.decompose(),rebuilt=compose(parts.own,parts.children);
  assert.equal(rebuilt.own(),rootData);assert.equal(rebuilt.at([b('00')]).own(),childData);
  assert.equal(rebuilt.at([b('00')]).at([]),rebuilt.at([b('00')]));
});
test('construction validates references without reading and permits generic undefined payload',()=>{
  let reads=0;
  const source={async read(){reads++;return b('ff')}};
  const child=dataTree(source),root=dataTree(source,[[b(''),child]]);
  assert.equal(root.own(),source);assert.equal(reads,0);
  for(const value of [undefined,null,{}]) assert.throws(()=>dataTree(value),isCode('invalid_data'));
  for(const value of [undefined,null]) assert.throws(()=>compose(source,[[b(''),value]]),isCode('invalid_child'));
  assert.equal(compose(undefined).own(),undefined);assert.equal(reads,0);
});
test('derived read distinguishes missing paths from failed storage and copies successful reads',async()=>{
  const missing=new StoreError('not_found'),unavailable=dataTree({async read(){throw missing}});
  await assert.rejects(read(unavailable),e=>e===missing);
  await assert.rejects(read(unavailable,[b('')]),e=>e!==missing && e.code==='path_not_found');
  const bytes=b('ff'),source=dataTree({async read(){return bytes}});
  const result=await read(source);result[0]=0;
  assert.equal(hx(bytes),'ff');assert.equal(hx(await read(source)),'ff');
});
test('codecs materialize read capabilities and preserve frozen byte fixtures',async()=>{
  const reads=[];
  const d=dataTree({async read(){reads.push('read');return b('')}});
  assert.equal(hx(await encodeFlat(d)),'647866320200010000');
  assert.deepEqual(reads,['read']);
  const {root,chunks}=await encodeLinked(d);
  assert.deepEqual(root,file.vectors.find(v=>v.label==='empty mandatory own and no children').root);
  assert.equal(chunks.size,1);assert.deepEqual(reads,['read','read']);
  const failure=new Error('unavailable'),unavailable=dataTree({async read(){throw failure}});
  await assert.rejects(read(unavailable),e=>e===failure);
  await assert.rejects(encodeFlat(unavailable),e=>e===failure);
  await assert.rejects(encodeLinked(unavailable),e=>e===failure);
});
test('canonical encoding accepts independently implemented structural nodes',async()=>{
  const low=tree(b('00')),high=tree(b('ff'));
  const other={
    own:()=>bytesData(b('72')),
    children:()=>[[b('ff'),high],[b('00'),low]],
    at(path){return select(this,path)},
    decompose(){return {own:this.own(),children:this.children()}}
  };
  const canonical=tree(b('72'),[[b('00'),low],[b('ff'),high]]);
  assert.deepEqual(await encodeFlat(other),await encodeFlat(canonical));
  assert.deepEqual(await encodeLinked(other),await encodeLinked(canonical));
  const cyclic={own:()=>bytesData(),children:()=>[[b(''),cyclic]],at(path){return select(this,path)},decompose(){return {own:this.own(),children:this.children()}}};
  await assert.rejects(encodeFlat(cyclic),isCode('cyclic_tree'));
  await assert.rejects(encodeLinked(cyclic),isCode('cyclic_tree'));
});
test('cancellation is observed after pending primitive reads before persistence',async()=>{
  const controller=new AbortController(),store=new MemoryStore();let writes=0;
  store.put=async()=>{writes++;throw new Error('unexpected write')};
  const d=dataTree({async read(){controller.abort();return b('ff')}});
  await assert.rejects(putDataTree(store,d,{},controller.signal),{name:'AbortError'});
  assert.equal(writes,0);
});
test('lazy paths, absence and lying stores',async()=>{
  const store=new MemoryStore(),leaf=tree(b('ff')),d=tree(b('72'),[[b(''),tree(b(''),[[b('ff'),leaf]])],[b('73'),leaf]]),root=await putDataTree(store,d);
  const raw=store.getStream.bind(store),reads=[];store.getStream=async(n,s)=>{reads.push(n);return raw(n,s)};
  const v=await openDataTree(store,root);assert.equal(reads.length,1);assert.equal(await v.at([]),v);assert.equal(await v.at([b('77')]),undefined);assert.equal(reads.length,1);
  assert.equal(hx((await v.at([b(''),b('ff')])).own()),'ff');assert.equal(reads.length,3);
  store.getStream=async()=>{throw new StoreError('not_found')};await assert.rejects(v.at([b('73')]),isCode('not_found'));
  store.getStream=async()=>new ReadableStream({start(c){c.enqueue(b('00'));c.close()}});await assert.rejects(openDataTree(store,root),isCode('integrity'));
  store.put=async()=>nameOf(b('01'));await assert.rejects(putDataTree(store,d),isCode('integrity'));
  await assert.rejects(openDataTree(store,{...root,profile:'future'}),isCode('unsupported_profile'));
});
test('bounded logical unfolding even with shared chunks',async()=>{
  let d=tree(b('ff'));for(let i=0;i<12;i++)d=tree(b(''),[[b('00'),d],[b('01'),d]]);
  const store=new MemoryStore(),root=await putDataTree(store,d);
  await assert.rejects(loadDataTree(store,root,{nodes:100}),isCode('limit_exceeded'));
  await assert.rejects(encodeLinked(d,{nodes:100}),isCode('limit_exceeded'));
  await assert.rejects(loadDataTree(store,root,{uniqueChunks:2}),isCode('limit_exceeded'));
  await assert.rejects(loadDataTree(store,root,{chunkBytes:8}),isCode('limit_exceeded'));
  await assert.rejects(encodeFlat(d,{depth:2}),isCode('limit_exceeded'));
  const abort=new AbortController();abort.abort();await assert.rejects(putDataTree(store,d,{},abort.signal),{name:'AbortError'});
});
test('linked framing and child mismatch precedence',async()=>{
  const store=new MemoryStore(),h=(await nameOf(b('ff'))).slice(7),h2=(await nameOf(b(''))).slice(7);
  for(const [hex,code] of [
    ['64786c3202000101'+h+'0001016101','bad_link_index'],['64786c3202000101'+h+'0000','unused_link'],
    ['64786c3202000102'+h+h+'0000','duplicate_link_hash'],['64786c3202000102'+h+h2+'00010001','links_out_of_order'],
    ['64786c320200020001','unexpected_eof'],['64786c32020002000000','unsupported_slot_codec']
  ]) {const n=await store.put(b(hex));await assert.rejects(openDataTree(store,{profile:dataProfile,address:'dxl2:'+n.slice(7)}),isCode(code));}
  for(const [body,code] of [['01','unexpected_eof'],['0000','slot_codec_mismatch']]) {
    const n=await store.put(b('64786c3202000200'+body)),pn=await store.put(b('64786c3202000101'+n.slice(7)+'00010000'));
    const v=await openDataTree(store,{profile:dataProfile,address:'dxl2:'+pn.slice(7)});await assert.rejects(v.at([b('')]),isCode(code));
  }
});
test('stream upload errors, size refusals and cancellation commit nothing',async()=>{
  const s=new MemoryStore(4),blob=b('010203'),n=await nameOf(blob);let pulls=0;
  const broken=new ReadableStream({pull(c){if(pulls++===0)c.enqueue(blob);else c.error(new Error('broken'))}});
  await assert.rejects(s.putStream(broken),/broken/);assert.deepEqual(await s.has([n]),[]);
  let cancelled=false;const endless=new ReadableStream({pull(c){c.enqueue(blob)},cancel(){cancelled=true}});
  await assert.rejects(s.putStream(endless),isCode('too_large'));assert.equal(cancelled,true);
  const abort=new AbortController();const stalled=new ReadableStream({cancel(){cancelled=true}});const promise=s.putStream(stalled,abort.signal);abort.abort();await assert.rejects(promise,{name:'AbortError'});
  const outcomes=await s.putMany([blob,b('0102030405'),b('')]);assert.equal(outcomes[0].name,n);assert.equal(outcomes[1].error.code,'too_large');assert.equal(outcomes[2].size,0);
});

test('flat unfolded budget counts shared child occurrences',async()=>{
  const leaf=tree(b('78')),d=tree(b(''),[[b('61'),leaf],[b('62'),leaf]]);
  await assert.rejects(encodeFlat(d,{unfoldedBytes:16}),e=>e.dimension==='unfolded_flat_octets');
  await assert.rejects(encodeFlat(tree(),{flatBytes:8}),e=>e.dimension==='flat_artifact_octets');
});
