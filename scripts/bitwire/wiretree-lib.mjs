// The full-tree family: validation of conformance/wiretree/cases.json, driver
// inputs with every expectation withheld, and comparison of driver observations
// with the independently authored oracle. Keys reach drivers only as lowercase
// hex of their exact bytes, so no driver parses the case notation.
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { isDeepStrictEqual } from 'node:util';

const encoder = new TextEncoder();
const families = new Set(['structure', 'bridge', 'carrier']);
const operations = {
  structure: new Set(['structure', 'send', 'same', 'rebuild', 'own', 'substitute', 'omit', 'rename', 'add', 'replace', 'view']),
  bridge: new Set(['bridge', 'bridgeInvalid']),
  carrier: new Set(['send', 'sendAddressed', 'structure', 'rebuild', 'replace', 'hold', 'release', 'cancel', 'teardown', 'direct']),
};
const faults = new Set(['duplicate', 'missingChild', 'cycle']);

function scalar(text) {
  assert.equal(typeof text, 'string');
  assert.ok(text.isWellFormed(), `not a Unicode scalar string: ${JSON.stringify(text)}`);
  return text;
}

/** Exact key bytes as lowercase hex: a string is its UTF-8; {hex} is bytes. */
export function keyHex(key) {
  if (typeof key === 'string') return Buffer.from(encoder.encode(scalar(key))).toString('hex');
  assert.ok(key && typeof key === 'object' && Object.keys(key).join() === 'hex', `invalid key ${JSON.stringify(key)}`);
  assert.match(key.hex, /^(?:[0-9a-f]{2})*$/, `invalid hex key ${key.hex}`);
  return key.hex;
}
const pathHex = path => {
  assert.ok(Array.isArray(path), 'a tree path is an array of keys');
  return path.map(keyHex);
};
const addressed = path => {
  assert.ok(Array.isArray(path), 'an addressed path is an array of strings');
  return path.map(scalar);
};

export function validateWiretree(fixture) {
  assert.equal(fixture.schemaVersion, 1);
  assert.ok(Array.isArray(fixture.servedAt) && fixture.servedAt.length > 0, 'servedAt must be a nonempty addressed prefix');
  addressed(fixture.servedAt);
  const declarations = new Map();
  for (const d of fixture.declarations) {
    assert.ok(typeof d.id === 'string' && !declarations.has(d.id), `duplicate or unnamed declaration ${d.id}`);
    assert.ok(d.own === null || (typeof d.own === 'string' && d.own.length > 0), `${d.id}: own is a name or null`);
    declarations.set(d.id, d);
  }
  for (const d of fixture.declarations) {
    const keys = (d.children ?? []).map(([key, child]) => {
      assert.ok(declarations.has(child), `${d.id}: unknown child ${child}`);
      return keyHex(key);
    });
    assert.equal(new Set(keys).size, keys.length, `${d.id}: duplicate byte key`);
  }
  assert.ok(Array.isArray(fixture.cases) && fixture.cases.length > 0);
  const ids = new Set();
  for (const test of fixture.cases) {
    assert.ok(typeof test.id === 'string' && !ids.has(test.id), `duplicate case ${test.id}`);
    ids.add(test.id);
    assert.ok(families.has(test.family), `${test.id}: unknown family ${test.family}`);
    assert.ok(declarations.has(test.root), `${test.id}: unknown root`);
    assert.ok(test.fault === undefined || (test.family === 'structure' && faults.has(test.fault)), `${test.id}: unknown fault`);
    assert.ok((!test.relay && !test.mount) || test.family === 'carrier', `${test.id}: relay and mount are carrier options`);
    assert.ok(!test.foreign || test.family === 'structure', `${test.id}: foreign is a structure option`);
    for (const step of test.steps) {
      assert.ok(operations[test.family].has(step.op), `${test.id}: ${step.op} is not a ${test.family} operation`);
      if (step.node !== undefined) assert.ok(declarations.has(step.node), `${test.id}: unknown node ${step.node}`);
      if (step.op === 'bridgeInvalid') {
        // One segment that is not a Unicode scalar string, in both representations.
        assert.ok(Array.isArray(step.utf16) && step.utf16.every(unit => /^[0-9a-f]{4}$/.test(unit)), `${test.id}: bridgeInvalid needs utf16 code units`);
        assert.ok(!String.fromCharCode(...step.utf16.map(unit => parseInt(unit, 16))).isWellFormed(), `${test.id}: the utf16 segment is well formed`);
        assert.match(step.utf8, /^(?:[0-9a-f]{2})+$/, `${test.id}: bridgeInvalid needs utf8 bytes`);
        assert.throws(() => new TextDecoder('utf-8', { fatal: true }).decode(Buffer.from(step.utf8, 'hex')), `${test.id}: the utf8 segment is valid UTF-8`);
      }
      if (test.family === 'carrier' && ['rebuild', 'replace'].includes(step.op)) {
        assert.ok(['near', 'far'].includes(step.side), `${test.id}: ${step.op} names its side`);
      }
    }
    assert.ok(test.expected && typeof test.expected === 'object', `${test.id}: no expectation`);
  }
  return declarations;
}

function stepInput(step) {
  const out = { ...step };
  for (const field of ['path', 'keep', 'selections', 'paths']) {
    if (step[field] === undefined) continue;
    if (field === 'path' && ['bridge', 'sendAddressed'].includes(step.op)) out.path = addressed(step.path);
    else if (field === 'path') out.path = pathHex(step.path);
    else out[field] = step[field].map(pathHex);
  }
  for (const field of ['key', 'to']) if (step[field] !== undefined) out[field] = keyHex(step[field]);
  return out;
}

/** What a driver receives: structure and steps, never an expectation. */
export function wiretreeInputs(fixture) {
  validateWiretree(fixture);
  return {
    servedAt: fixture.servedAt,
    declarations: fixture.declarations.map(({ id, own, children }) => ({
      id, own, children: (children ?? []).map(([key, child]) => [keyHex(key), child]),
    })),
    cases: fixture.cases.map(({ id, family, root, fault, foreign, relay, mount, steps }) => ({
      id, family, root, ...(fault ? { fault } : {}), ...(foreign ? { foreign } : {}), ...(relay ? { relay } : {}), ...(mount ? { mount } : {}),
      steps: steps.map(stepInput),
    })),
  };
}

/** Children are a map, not a list: order is canonicalized by exact key bytes. */
function canonicalRender(render, renders, keyed) {
  if (render === 'missing' || render === 'unknown') return render;
  if (render && typeof render === 'object' && Object.keys(render).join() === '$render') {
    assert.ok(Object.hasOwn(renders, render.$render), `unknown render ${render.$render}`);
    return canonicalRender(renders[render.$render], renders, keyed);
  }
  assert.ok(render && typeof render === 'object' && Array.isArray(render.children), `invalid render ${JSON.stringify(render)}`);
  const children = render.children.map(([key, child]) => [keyed ? keyHex(key) : key, canonicalRender(child, renders, keyed)]);
  children.sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0));
  return { own: render.own, children };
}
function canonical(observation, renders, keyed) {
  if (!observation || typeof observation !== 'object' || !Array.isArray(observation.trace)) return observation;
  return {
    ...observation,
    trace: observation.trace.map(entry => entry[0] === 'structure'
      ? ['structure', canonicalRender(entry[1], renders, keyed)] : entry),
  };
}

export function wiretreeExpected(fixture) {
  validateWiretree(fixture);
  return new Map(fixture.cases.map(test => [test.id, canonical(test.expected, fixture.renders ?? {}, true)]));
}

/** Compares the cases of the given families; missing, extra and duplicate rows fail. */
export function compareWiretree(fixture, actual, selected, label) {
  const expected = wiretreeExpected(fixture);
  const cases = fixture.cases.filter(test => selected.includes(test.family));
  assert.ok(Array.isArray(actual), `${label}: expected an observations array`);
  for (const row of actual) assert.deepEqual(Object.keys(row).sort(), ['id', 'observations'], `${label}: invalid result record`);
  const rows = new Map(actual.map(row => [row.id, row.observations]));
  assert.equal(rows.size, actual.length, `${label}: duplicate observations`);
  assert.equal(actual.length, cases.length, `${label}: missing or extra observations`);
  for (const test of cases) {
    assert.ok(rows.has(test.id), `${label}: missing ${test.id}`);
    assert.deepEqual(canonical(rows.get(test.id), {}, false), expected.get(test.id), `${label}: ${test.id}`);
  }
  return cases.length;
}

/** The cases of the given families that an implementation fails; used to show unlawful realizations are rejected. */
export function wiretreeFailures(fixture, actual, selected) {
  const expected = wiretreeExpected(fixture);
  const rows = new Map(actual.map(row => [row.id, row.observations]));
  return fixture.cases.filter(test => selected.includes(test.family))
    .filter(test => !isDeepStrictEqual(canonical(rows.get(test.id), {}, false), expected.get(test.id)))
    .map(test => test.id);
}

const sha256 = bytes => createHash('sha256').update(bytes).digest('hex');

/**
 * Every historical declared case, recorded gap and limitation is disposed
 * exactly once, and every current case it names exists. The historical files
 * are pinned by digest: changing them reopens the disposition.
 */
export function validateDisposition(disposition, fixture, read) {
  assert.equal(disposition.schemaVersion, 1);
  for (const [path, digest] of Object.entries(disposition.historical)) {
    assert.equal(sha256(read(path)), digest, `${path} changed; the disposition must be revisited`);
  }
  const declared = JSON.parse(read('conformance/declared/cases.json'));
  const ledgers = ['conformance/declared/production-gaps.json', 'conformance/runtime/production-gaps.json'].map(path => JSON.parse(read(path)));
  const current = new Map(fixture.cases.map(test => [test.id, test]));
  const kinds = new Set(Object.keys(disposition.kinds));
  const limitations = new Set(Object.keys(disposition.limitations));
  const familyOf = { structural: 'structure', bridge: 'bridge', carrier: 'carrier' };
  const cited = new Set();
  const names = cases => {
    for (const id of cases) {
      assert.ok(current.has(id), `unknown current case ${id}`);
      cited.add(id);
    }
  };

  const disposed = new Map(disposition.cases.map(row => [row.id, row]));
  assert.equal(disposed.size, disposition.cases.length, 'a declared case is disposed twice');
  assert.deepEqual([...disposed.keys()].sort(), declared.cases.map(test => test.id).sort(), 'every declared case is disposed exactly once');
  const gapCases = new Map();
  for (const ledger of ledgers) for (const gap of ledger.gaps) for (const id of gap.cases) gapCases.set(id, gap.id);
  for (const row of disposition.cases) {
    assert.equal(row.gap, gapCases.get(row.id), `${row.id}: gap membership differs from the ledgers`);
    assert.ok(row.requirements.length > 0, `${row.id}: no requirement`);
    for (const requirement of row.requirements) {
      assert.ok(kinds.has(requirement.kind), `${row.id}: unknown kind ${requirement.kind}`);
      if (requirement.kind === 'historical-addressed') {
        assert.ok(limitations.has(requirement.limitation), `${row.id}: unknown limitation ${requirement.limitation}`);
        assert.equal(requirement.cases, undefined, `${row.id}: a historical limitation names no current case`);
      } else {
        assert.ok(requirement.cases?.length > 0, `${row.id}: ${requirement.kind} names no current case`);
        names(requirement.cases);
        for (const id of requirement.cases) assert.equal(current.get(id).family, familyOf[requirement.kind], `${row.id}: ${id} is not ${requirement.kind}`);
      }
    }
  }

  for (const ledger of ledgers) {
    assert.deepEqual(ledger.gaps.map(gap => gap.id).sort(), disposition.gaps.map(gap => gap.id).sort(), 'every recorded gap is disposed');
    assert.deepEqual(ledger.limitations.map(item => item.id).sort(), disposition.ledgerLimitations.map(item => item.id).sort(), 'every recorded limitation is disposed');
  }
  for (const item of [...disposition.gaps, ...disposition.ledgerLimitations]) {
    names(item.cases);
    if (item.limitation) assert.ok(limitations.has(item.limitation), `${item.id}: unknown limitation`);
  }
  const uncited = fixture.cases.map(test => test.id).filter(id => !cited.has(id));
  return { declared: declared.cases.length, gaps: disposition.gaps.length, gapCases: gapCases.size, limitations: disposition.ledgerLimitations.length, uncited };
}
