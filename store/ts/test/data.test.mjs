import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {Data, MemoryStore, StoreError, CodecError, nameOf, encodeFlat, decodeFlat, encodeLinked, putData, openData, loadData, dataProfile} from '../dist/index.js';
const file=JSON.parse(readFileSync(new URL('../../../vectors/data.json',import.meta.url)));
const b=s=>Uint8Array.from(Buffer.from(s,'hex'));
const hx=b=>Buffer.from(b).toString('hex');
const isCode=code=>e=>e.code===code;
for(const v of file.vectors) test(v.label,async()=>{
  const d=decodeFlat(b(v.flat)); assert.equal(hx(encodeFlat(d)),v.flat);
  const {root,chunks}=await encodeLinked(d);assert.deepEqual(root,v.root);assert.deepEqual(Object.fromEntries([...chunks].map(([n,b])=>[n,hx(b)])),v.chunks);
  const store=new MemoryStore();for(const [n,hex] of Object.entries(v.chunks)) assert.equal(await store.put(b(hex)),n);
  assert.equal(hx(encodeFlat(await loadData(store,v.root))),v.flat);
});
for(const v of file.invalidFlat) test(v.label,()=>assert.throws(()=>decodeFlat(b(v.hex)),isCode(v.code)));
test('exact keys, immutable own and children, reconstruction',()=>{
  const own=b('ff'),key=b('ff'),leaf=new Data();const d=new Data(own,[[key,leaf],[b(''),leaf]]);own[0]=0;key[0]=0;d.own()[0]=1;d.children()[1][0][0]=1;
  assert.equal(hx(d.own()),'ff');assert.equal(d.at(b('ff')),leaf);assert.equal(d.at(),d);assert.equal(d.at(b('')),leaf);assert.equal(d.at(b('00')),undefined);
  assert.deepEqual(encodeFlat(new Data(d.own(),d.children())),encodeFlat(d));assert.throws(()=>new Data(b(''),[[b(''),leaf],[b(''),leaf]]));
});
test('lazy paths, absence and lying stores',async()=>{
  const store=new MemoryStore(),leaf=new Data(b('ff')),d=new Data(b('72'),[[b(''),new Data(b(''),[[b('ff'),leaf]])],[b('73'),leaf]]),root=await putData(store,d);
  const raw=store.getStream.bind(store),reads=[];store.getStream=async(n,s)=>{reads.push(n);return raw(n,s)};
  const v=await openData(store,root);assert.equal(reads.length,1);assert.equal(await v.at([]),v);assert.equal(await v.at([b('77')]),undefined);assert.equal(reads.length,1);
  assert.equal(hx((await v.at([b(''),b('ff')])).own()),'ff');assert.equal(reads.length,3);
  store.getStream=async()=>{throw new StoreError('not_found')};await assert.rejects(v.at([b('73')]),isCode('not_found'));
  store.getStream=async()=>new ReadableStream({start(c){c.enqueue(b('00'));c.close()}});await assert.rejects(openData(store,root),isCode('integrity'));
  store.put=async()=>nameOf(b('01'));await assert.rejects(putData(store,d),isCode('integrity'));
  await assert.rejects(openData(store,{...root,profile:'future'}),isCode('unsupported_profile'));
});
test('bounded logical unfolding even with shared chunks',async()=>{
  let d=new Data(b('ff'));for(let i=0;i<12;i++)d=new Data(b(''),[[b('00'),d],[b('01'),d]]);
  const store=new MemoryStore(),root=await putData(store,d);
  await assert.rejects(loadData(store,root,{nodes:100}),isCode('limit_exceeded'));
  await assert.rejects(encodeLinked(d,{nodes:100}),isCode('limit_exceeded'));
  await assert.rejects(loadData(store,root,{uniqueChunks:2}),isCode('limit_exceeded'));
  await assert.rejects(loadData(store,root,{chunkBytes:8}),isCode('limit_exceeded'));
  assert.throws(()=>encodeFlat(d,{depth:2}),isCode('limit_exceeded'));
  const abort=new AbortController();abort.abort();await assert.rejects(putData(store,d,{},abort.signal),{name:'AbortError'});
});
test('linked framing and child mismatch precedence',async()=>{
  const store=new MemoryStore(),h=(await nameOf(b('ff'))).slice(7),h2=(await nameOf(b(''))).slice(7);
  for(const [hex,code] of [
    ['64786c3202000101'+h+'0001016101','bad_link_index'],['64786c3202000101'+h+'0000','unused_link'],
    ['64786c3202000102'+h+h+'0000','duplicate_link_hash'],['64786c3202000102'+h+h2+'00010001','links_out_of_order'],
    ['64786c320200020001','unexpected_eof'],['64786c32020002000000','unsupported_slot_codec']
  ]) {const n=await store.put(b(hex));await assert.rejects(openData(store,{profile:dataProfile,address:'dxl2:'+n.slice(7)}),isCode(code));}
  for(const [body,code] of [['01','unexpected_eof'],['0000','slot_codec_mismatch']]) {
    const n=await store.put(b('64786c3202000200'+body)),pn=await store.put(b('64786c3202000101'+n.slice(7)+'00010000'));
    const v=await openData(store,{profile:dataProfile,address:'dxl2:'+pn.slice(7)});await assert.rejects(v.at([b('')]),isCode(code));
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
