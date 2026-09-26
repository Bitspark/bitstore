// One-time, specification-authored fixtures. This script does not import a
// Bitstore implementation. Once published, append vectors; never regenerate
// existing values from an encoder under test.
import {createHash} from 'node:crypto';
import {writeFileSync} from 'node:fs';
const name = hex => 'sha256:'+createHash('sha256').update(Buffer.from(hex,'hex')).digest('hex');
const empty='64786c32020001000000';
const ff='64786c320200010001ff00';
// magic, id length, identity id, 2 first-use hashes, own r, 3 key/index pairs.
const parent='64786c3202000102'+name(empty).slice(7)+name(ff).slice(7)+'017203000001000101ff01';
const vector=(label,flat,chunks) => ({label,flat,root:{profile:'deixis-codec-v2/identity-bytes@v0.4.0',address:'dxl2:'+name(chunks.at(-1)).slice(7)},chunks:Object.fromEntries(chunks.map(b=>[name(b),b]))});
const vectors=[
  vector('empty mandatory own and no children','647866320200010000',[empty]),
  vector('opaque ff own bytes','6478663202000101ff00',[ff]),
  vector('empty key, binary keys and repeated whole child','64786632020001017203000000010001ff0001ff01ff00',[empty,ff,parent])
];
const invalidFlat=[
  ['short magic','64','unexpected_eof'],
  ['wrong magic','000000000200010000','unknown_magic'],
  ['cut off varint','6478663280','malformed_uvarint'],
  ['non shortest length','64786632820000010000','non_shortest_uvarint'],
  ['overflow length','64786632ffffffffffffffffff02','uvarint_overflow'],
  ['overlong varint','647866328080808080808080808000','malformed_uvarint'],
  ['public zero id','647866320200000000','malformed_slot_codec_id'],
  ['bounded id cut integer','647866320200800000','malformed_uvarint'],
  ['unsupported id malformed body','6478663202000201','unexpected_eof'],
  ['unsupported id canonical body','647866320200020000','unsupported_slot_codec'],
  ['duplicate empty keys','647866320200010002000000000000','duplicate_key'],
  ['unsorted binary keys','64786632020001000201ff000001000000','unsorted_keys'],
  ['trailing bytes','64786632020001000000','trailing_bytes'],
  ['truncated framed key wins over ordering','64786632020001000201ff00000200','unexpected_eof']
].map(([label,hex,code])=>({label,hex,code}));
writeFileSync(new URL('../vectors/data.json',import.meta.url),JSON.stringify({provenance:'Hand-authored from identity-bytes grammar; SHA-256 computed with node:crypto, not Bitstore.',vectors,invalidFlat},null,2)+'\n');
