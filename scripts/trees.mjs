// Runs bitwire's independently authored structural cases against bitstore's
// DataTree, in Go and TypeScript, from this checkout's source.
//
// The cases and bitwire's comparison library are vendored byte-identical and
// pinned by digest in vectors/bitwire/wiretree/SOURCE.json. Drivers receive the
// inputs with every expectation withheld; this script compares complete
// observations. Deliberately unlawful realizations must each fail the case
// aimed at them. See docs/TREES.md.
//
//   node scripts/trees.mjs              offline: verify the pin, build, run
//   node scripts/trees.mjs --upstream   also check the pin against GitHub
import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {createHash} from 'node:crypto';
import {mkdtempSync, readFileSync, rmSync, writeFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {dirname, join, resolve} from 'node:path';
import {fileURLToPath, pathToFileURL} from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const sha256 = data => createHash('sha256').update(data).digest('hex');
const source = JSON.parse(readFileSync(join(root, 'vectors/bitwire/wiretree/SOURCE.json'), 'utf8'));
const pin = `bitwire@${source.commit.slice(0, 7)}`;

// Deliberately unlawful realizations and the case each must fail.
const rejected = [
  ['fallback', 'missing-never-falls-back'],
  ['fabricating', 'refusing-versus-missing'],
  ['empty-for-missing', 'missing-never-falls-back'],
  ['missing-as-failure', 'refusing-versus-missing'],
  ['swallowing', 'own-and-descendants'],
  ['corrupting', 'own-and-descendants'],
  ['aliasing', 'own-and-descendants'],
  ['failing-after-read', 'own-and-descendants'],
  ['wrapping-own', 'root-cut-reconstruction'],
  ['incomplete-children', 'own-and-descendants'],
  ['normalizing', 'own-and-descendants', ['ts']],
  ['lossy-keys', 'own-and-descendants'],
  ['cycle-accepting', 'cycle-refused'],
];
// Unlawful realizations the pinned cases cannot yet detect. Each must still pass
// every case; when a re-pin starts rejecting one, move it to the list above.
const gaps = [
  ['bom-stripping', 'no key begins with a UTF-8 byte order mark', 'https://github.com/Bitspark/bitwire/issues/65'],
];

for (const file of source.files) {
  assert.equal(sha256(readFileSync(join(root, file.path))), file.sha256, `${file.path} is not the pinned ${pin} ${file.upstream}`);
}
if (process.argv.includes('--upstream')) {
  for (const file of source.files) {
    const url = `https://raw.githubusercontent.com/Bitspark/bitwire/${source.commit}/${file.upstream}`;
    const response = await fetch(url);
    assert.ok(response.ok, `${url}: HTTP ${response.status}`);
    assert.equal(sha256(Buffer.from(await response.arrayBuffer())), file.sha256, `${file.path} differs from ${url}`);
  }
  console.log(`Pin verified against GitHub: ${pin}.`);
}
// Load the comparison only once its bytes are known to be bitwire's.
const {compareWiretree, wiretreeFailures, wiretreeInputs} = await import(pathToFileURL(join(root, 'scripts/bitwire/wiretree-lib.mjs')).href);
const fixture = JSON.parse(readFileSync(join(root, 'vectors/bitwire/wiretree/cases.json'), 'utf8'));
const count = family => fixture.cases.filter(test => test.family === family).length;

const run = (command, args) => {
  const result = spawnSync(command, args, {cwd: root, encoding: 'utf8', maxBuffer: 64 << 20});
  assert.equal(result.status, 0, `${command} ${args.join(' ')} failed:\n${result.error ?? ''}${result.stderr}`);
  return result.stdout;
};
const scratch = mkdtempSync(join(tmpdir(), 'bitstore-trees-'));
try {
  const inputs = join(scratch, 'inputs.json');
  writeFileSync(inputs, JSON.stringify(wiretreeInputs(fixture)));
  const tsc = join(root, 'store/ts/node_modules/typescript/bin/tsc');
  run(process.execPath, [tsc, '-p', 'store/ts']);
  const exe = join(scratch, process.platform === 'win32' ? 'trees.exe' : 'trees');
  run('go', ['build', '-o', exe, './conformance/go/trees']);
  const drivers = {
    go: {observe: realization => JSON.parse(run(exe, [realization, inputs])), through: 'NewDataTree, Select and Read'},
    ts: {observe: realization => JSON.parse(run(process.execPath, ['conformance/ts/trees.mjs', realization, inputs])), through: 'dataTree, select and read'},
  };

  const lines = [];
  for (const [language, driver] of Object.entries(drivers)) {
    const passed = compareWiretree(fixture, driver.observe('production'), ['structure'], `${language}/production`);
    lines.push(`${language}/production: ${passed}/${passed} structure cases through ${driver.through}`);
    for (const [mutant, target, languages = ['go', 'ts']] of rejected) {
      if (!languages.includes(language)) continue;
      const failed = wiretreeFailures(fixture, driver.observe(mutant), ['structure']);
      assert.ok(failed.includes(target), `${language}: the unlawful realization ${mutant} passes ${target} (fails: ${failed.join(', ') || 'nothing'})`);
      const others = failed.length - 1;
      lines.push(`${language}/mutant/${mutant}: rejected by ${target}${others ? ` and ${others} other case${others === 1 ? '' : 's'}` : ''}`);
    }
    for (const [mutant, reason, tracking] of gaps) {
      const failed = wiretreeFailures(fixture, driver.observe(mutant), ['structure']);
      assert.deepEqual(failed, [], `${language}: the pinned cases now reject ${mutant}; move it from the gaps to the rejected realizations`);
      lines.push(`${language}/gap/${mutant}: passes every case, because ${reason} (${tracking})`);
    }
  }
  console.log(`Structural cases: ${pin} ${source.files[0].upstream}, sha256 ${source.files[0].sha256.slice(0, 12)}.`);
  console.log(`Run: the ${count('structure')} structure cases. Excluded: ${count('bridge')} bridge and ${count('carrier')} carrier cases (Wire-specific).`);
  for (const line of lines) console.log(`  ${line}`);
} finally {
  rmSync(scratch, {recursive: true, force: true});
}
