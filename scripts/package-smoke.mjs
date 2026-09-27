import {spawnSync} from 'node:child_process';
import {mkdtempSync,writeFileSync,copyFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join,resolve} from 'node:path';
const temp=mkdtempSync(join(tmpdir(),'bitstore-consumer-'));
const npm=process.platform==='win32'?'npm.cmd':'npm';
function run(cmd,args,cwd,encoding){const r=spawnSync(cmd,args,{cwd,shell:process.platform==='win32'&&cmd.endsWith('.cmd'),encoding,stdio:encoding?'pipe':'inherit'});if(r.status!==0)throw Error(`${cmd} ${args.join(' ')} failed: ${r.stderr??''}`);return r.stdout;}
for(const file of ['LICENSE','NOTICE'])copyFileSync(file,join('store/ts',file));
const packed=JSON.parse(run(npm,['pack','--json','--pack-destination',temp],resolve('store/ts'),'utf8'));
// npm 11 reports an array of packages; npm 12 an object keyed by package name.
const [artifact]=Array.isArray(packed)?packed:Object.values(packed);
writeFileSync(join(temp,'package.json'),'{"type":"module","private":true}\n');
run(npm,['install','--ignore-scripts','--no-audit',join(temp,artifact.filename)],temp);
writeFileSync(join(temp,'smoke.mjs'),`import {dataTree,bytesData,MemoryStore,putDataTree,loadDataTree,encodeFlat} from '@bitspark/bitstore';
import assert from 'node:assert/strict';
const data=dataTree(bytesData(Uint8Array.of(255)),[[new Uint8Array(),dataTree(bytesData())]]),store=new MemoryStore();
const root=await putDataTree(store,data);assert.deepEqual(await encodeFlat(await loadDataTree(store,root)),await encodeFlat(data));
console.log('Fresh packed npm consumer passed.');\n`);
run(process.execPath,['smoke.mjs'],temp);
console.log('Package artifact: '+join(temp,artifact.filename));
