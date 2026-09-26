import {spawnSync} from 'node:child_process';
import {mkdtempSync,readFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import assert from 'node:assert/strict';
import {Data,encodeFlat,encodeLinked,loadData,MemoryStore} from '../store/ts/dist/index.js';
const temp=mkdtempSync(join(tmpdir(),'bitstore-interop-')),exe=join(temp,process.platform==='win32'?'codec.exe':'codec');
const build=spawnSync('go',['build','-o',exe,'./cmd/bs-codec/go'],{stdio:'inherit'});if(build.status)process.exit(build.status);
const go=artifact=>{const r=spawnSync(exe,[],{input:JSON.stringify(artifact),encoding:'utf8',maxBuffer:32<<20});assert.equal(r.status,0,r.stderr);return JSON.parse(r.stdout)};
const hex=b=>Buffer.from(b).toString('hex'),unhex=s=>Uint8Array.from(Buffer.from(s,'hex'));
const file=JSON.parse(readFileSync(new URL('../vectors/data.json',import.meta.url)));
for(const v of file.vectors){const out=go({flat:v.flat});assert.deepEqual(out,{flat:v.flat,root:v.root,chunks:v.chunks});assert.deepEqual(go({root:out.root,chunks:out.chunks}),out);}
// Bounded deterministic values cover multi-byte lengths and keys with no text
// interpretation. These are interchange inputs, not golden expected addresses.
let seed=0x1eadbeef;const rand=()=>{seed=(Math.imul(seed,1664525)+1013904223)>>>0;return seed;};
const bytes=n=>Uint8Array.from({length:n},()=>rand()&255);
function tree(depth){const cs=[];for(let i=0;i<(depth?3:0);i++)cs.push([Uint8Array.of(i,rand()&255),tree(depth-1)]);return new Data(bytes(rand()%300),cs);}
for(let i=0;i<12;i++){
  const d=tree(i%4),flat=hex(encodeFlat(d)),native=await encodeLinked(d),out=go({flat});
  assert.deepEqual(out.root,native.root);assert.deepEqual(out.chunks,Object.fromEntries([...native.chunks].map(([n,b])=>[n,hex(b)])));
  const s=new MemoryStore();for(const [n,b]of Object.entries(out.chunks))assert.equal(await s.put(unhex(b)),n);
  assert.equal(hex(encodeFlat(await loadData(s,out.root))),flat);
  assert.equal(go({root:native.root,chunks:Object.fromEntries([...native.chunks].map(([n,b])=>[n,hex(b)]))}).flat,flat);
}
console.log('Go/TypeScript interchange passed: hand-authored fixtures and 12 constructed trees.');
