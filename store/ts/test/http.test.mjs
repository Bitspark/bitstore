import {test} from 'node:test';
import assert from 'node:assert/strict';
import {Client,discoverBytes,bytesProfile,contract,nameOf,MemoryStore} from '../dist/index.js';
import {readFileSync} from 'node:fs';
const bytes=s=>new TextEncoder().encode(s),code=c=>e=>e.code===c;
const response=(body,type='application/json',options={})=>new Response(body,{status:200,...options,headers:{'Bit-Service':'bitstore','Bit-Contract':contract,'Content-Type':type,...options.headers}});
const describe=()=>({service:'bitstore',contract,bindings:{primary:'http',http:{prefix:'/v1'}},profiles:[{...bytesProfile}]});
test('discovery uses explicit profile and keeps deployment prefix',async()=>{
  const d=describe();let requested;
  const options={fetch:async(url)=>{requested=url;return response(JSON.stringify(d))}};
  await discoverBytes('https://backend.test/prefix',options);assert.equal(requested,'https://backend.test/prefix/describe');
  for(const alter of [d=>d.profiles=[],d=>d.profiles.push({...bytesProfile}),d=>d.profiles[0].version='2',d=>d.profiles[0].binding='x',d=>d.surface='management',d=>d.contract='future']){
    const d=describe();alter(d);await assert.rejects(discoverBytes('https://backend.test',{fetch:async()=>response(JSON.stringify(d))}),code('protocol'));
  }
});
test('client rejects wrong identity, surface, malformed responses and lying content',async()=>{
  const b=bytes('hello'),n=await nameOf(b);
  for(const headers of [{'Bit-Service':'another'},{'Bit-Surface':'management'},{'Bit-Surface':'operations, operations'},{'Bit-Contract':''}]){
    await assert.rejects(new Client('https://backend.test',{fetch:async()=>response(b,'application/octet-stream',{headers})}).get(n),code('protocol'));
  }
  const corrupt=new Client('https://backend.test',{fetch:async()=>response(bytes('wrong'),'application/octet-stream')});await assert.rejects(corrupt.get(n),code('integrity'));
  const put=new Client('https://backend.test',{fetch:async()=>response(JSON.stringify({name:await nameOf(bytes('wrong')),size:5}))});await assert.rejects(put.put(b),code('integrity'));
  const stat=new Client('https://backend.test',{fetch:async()=>response(JSON.stringify({sizes:{[n]:-1}}))});await assert.rejects(stat.size([n]),code('protocol'));
  const wrongName=await nameOf(bytes('extra'));const unasked=new Client('https://backend.test',{fetch:async()=>response(JSON.stringify({sizes:{[wrongName]:0}}))});await assert.rejects(unasked.size([n]),code('protocol'));
  const batch=new Client('https://backend.test',{fetch:async()=>response(`${n} 5\nhello${n} 5\nhello`,'application/x-bitstore-blobs')});await assert.rejects(batch.getMany([n]),code('protocol'));
  const range=new Client('https://backend.test',{fetch:async()=>response(b,'application/octet-stream')});await assert.rejects(range.getRange(n,0,1),code('protocol'));
});
test('stream verification occurs at EOF and early close cancels response',async()=>{
  const b=bytes('hello'),n=await nameOf(b);let cancelled=false;
  const client=new Client('https://backend.test',{fetch:async()=>response(new ReadableStream({pull(c){c.enqueue(b)},cancel(){cancelled=true}}),'application/octet-stream')});
  const r=(await client.getStream(n)).getReader();assert.deepEqual((await r.read()).value,b);await r.cancel();assert.equal(cancelled,true);
  let calls=0;const upload=new Client('https://backend.test',{fetch:async()=>{calls++;return response('{}')}});
  await assert.rejects(upload.putStream(new ReadableStream({start(c){c.error(new Error('broken input'))}})),/broken input/);assert.equal(calls,0);
});
test('raw memory store holds independent SHA-256 vectors and nine verbs',async()=>{
  const {vectors}=JSON.parse(readFileSync(new URL('../../../conformance/go/vectors.json',import.meta.url)));
  const s=new MemoryStore(2<<20);
  for(const v of vectors){const b=v.content.base64!==undefined?Uint8Array.from(Buffer.from(v.content.base64,'base64')):Uint8Array.from({length:v.content.length},(_,i)=>i%256);
    assert.equal(await s.put(b),v.name);assert.deepEqual(await s.get(v.name),b);assert.equal((await s.size([v.name])).get(v.name),v.size);assert.deepEqual(await s.has([v.name,v.name]),[v.name]);assert.deepEqual((await s.getMany([v.name])).get(v.name),b);
    assert.deepEqual(await s.getRange(v.name,b.length+1,5),new Uint8Array());assert.deepEqual(await s.getRange(v.name,0,-1),b);
    const streamed=new Uint8Array(await new Response(await s.getStream(v.name)).arrayBuffer());assert.deepEqual(streamed,b);
    assert.deepEqual(await s.putStream(new ReadableStream({start(c){c.enqueue(b);c.close()}})),{name:v.name,size:b.length});
  }
  const missing=await nameOf(bytes('missing vector'));assert.deepEqual(await s.has([missing]),[]);assert.deepEqual(await s.getMany([missing]),new Map());await assert.rejects(s.get(missing),code('not_found'));
});
