// DataTree driver for bitwire's structural cases, run by scripts/trees.mjs.
// Expectations are withheld: this file interprets inputs and records what
// happened. "production" is bitstore's dataTree, select and read, built from
// store/ts; every other realization is deliberately unlawful and must fail.
//
// The cases are written for WireTree. A Data reader stands where the case says
// "send": the named reader was invoked and read ("delivered", with that reader
// instance's invocation count), or a present node's failing reader was invoked
// ("refused"). "missing" means selection found no node, no reader was invoked,
// and the derived read reported structural absence. docs/TREES.md has the full
// mapping.
import {readFileSync} from 'node:fs';
import {dataTree, read, select, StoreError} from '../../store/ts/dist/index.js';

const hex = key => Buffer.from(key).toString('hex');
const bytes = text => Uint8Array.from(Buffer.from(text, 'hex'));
const encoder = new TextEncoder();
const isMissing = error => error instanceof StoreError && error.code === 'path_not_found';
/** Whether a failure the caller saw is, or wraps, the reader's own failure. */
const surfaces = (got, want) => {
  for (let error = got, depth = 0; error && depth < 16; error = error.cause, depth++) if (error === want) return true;
  return false;
};
/** Selection by exact keys for driver-implemented nodes, independent of the realization. */
const walk = (tree, path) => {
  let node = tree;
  for (const key of path) node = node?.children().find(([candidate]) => hex(candidate) === hex(key))?.[1];
  return node;
};

// ---- Realizations ----
/** The implementation under test: bitstore's public DataTree operations. */
const production = {compose: (own, children) => dataTree(own, children), select, read};
/** A derived read built from a realization's own selection. */
const derivedFrom = T => async (tree, path) => {
  const node = T.select(tree, path);
  if (!node) throw new StoreError('path_not_found');
  return new Uint8Array(await node.own().read());
};
const withSelect = selectFn => {
  const T = {...production, select: selectFn};
  T.read = derivedFrom(T);
  return T;
};
const rekey = f => ({...production, compose: (own, children) => dataTree(own, children.map(([key, child]) => [f(key), child]))});
const strict = new TextDecoder('utf-8', {fatal: true, ignoreBOM: true});
const lossy = new TextDecoder('utf-8');

/** A minimal node without validation of foreign children: bitstore v0.2.0's construction. */
class Unvalidated {
  #own; #children;
  constructor(own, children) {
    const seen = new Set();
    this.#children = children.map(([key, child]) => {
      if (!child) throw new Error('invalid child');
      if (seen.has(hex(key))) throw new Error('duplicate key');
      seen.add(hex(key));
      return [Uint8Array.from(key), child];
    });
    this.#own = own;
  }
  own() { return this.#own; }
  children() { return this.#children.map(([key, child]) => [Uint8Array.from(key), child]); }
  at(path) { return walk(this, path); }
  decompose() { return {own: this.#own, children: this.children()}; }
}

const mutants = {
  /** A missing descendant is read from its deepest present ancestor. */
  fallback: {...production, async read(tree, path) {
    let node = tree;
    for (const key of path) node = select(node, [key]) ?? node;
    return new Uint8Array(await node.own().read());
  }},
  /** A missing path selects a fabricated node whose reader fails. */
  fabricating: withSelect((tree, path) => select(tree, path) ?? dataTree({read: async () => { throw new Error('fabricated'); }})),
  /** A missing path reads as empty content. */
  'empty-for-missing': {...production, read: async (tree, path) => select(tree, path) ? read(tree, path) : new Uint8Array()},
  /** A missing path is reported as a failed read rather than as absence. */
  'missing-as-failure': {...production, async read(tree, path) {
    if (!select(tree, path)) throw new Error('unreadable');
    return read(tree, path);
  }},
  /** A reader's failure is reported as empty content. */
  swallowing: {...production, async read(tree, path) {
    try { return await read(tree, path); } catch (error) { if (select(tree, path)) return new Uint8Array(); throw error; }
  }},
  /** The derived read alters the bytes it returns. */
  corrupting: {...production, read: async (tree, path) => Uint8Array.of(...await read(tree, path), 0)},
  /** The derived read hands the caller the reader's own buffer. */
  aliasing: {...production, async read(tree, path) {
    const node = select(tree, path);
    if (!node) throw new StoreError('path_not_found');
    return node.own().read();
  }},
  /** The derived read reports failure after its reader succeeded. */
  'failing-after-read': {...production, async read(tree, path) { await read(tree, path); throw new Error('lost'); }},
  /** The own reader is replaced by a forwarding wrapper. */
  'wrapping-own': {...production, compose: (own, children) => dataTree({read: () => own.read()}, children)},
  /** children() omits the empty key, so the child map is incomplete. */
  'incomplete-children': {...production, compose(own, children) {
    const node = dataTree(own, children);
    return {own: () => node.own(), at: path => node.at(path), decompose: () => node.decompose(),
      children: () => node.children().filter(([key]) => key.length > 0)};
  }},
  /** UTF-8 keys are NFC-normalized, conflating distinct byte keys. */
  normalizing: rekey(key => {
    try { return encoder.encode(strict.decode(key).normalize('NFC')); } catch { return key; }
  }),
  /** Keys pass through lossy text decoding: ff becomes U+FFFD and a leading BOM is dropped. */
  'lossy-keys': rekey(key => encoder.encode(lossy.decode(key))),
  /** Construction never looks for a cycle through a foreign child. */
  'cycle-accepting': {...production, compose: (own, children) => new Unvalidated(own, children)},
  /** A leading UTF-8 byte order mark is stripped from keys. */
  'bom-stripping': rekey(key => key[0] === 0xef && key[1] === 0xbb && key[2] === 0xbf ? key.subarray(3) : key),
};

// ---- Instrumented readers and structural editing ----
class Harness {
  trace = [];
  partsExact = true;
  unchanged = true;
  /** Reader invocations during the current derived read. */
  calls = [];
  names = new Map();
  refusers = new Set();
  #instances = new Map();
  #shared = new Map();
  #built = new Map();
  constructor(T, inputs) {
    this.T = T;
    this.declarations = new Map(inputs.declarations.map(d => [d.id, d]));
  }
  /** One stateful reader per name and case; a fresh instance only when a step asks. */
  primitive(name, fresh = false) {
    if (!fresh && this.#shared.has(name)) return this.#shared.get(name);
    const instance = (this.#instances.get(name) ?? 0) + 1;
    this.#instances.set(name, instance);
    let count = 0;
    const self = Object.freeze({read: async () => {
      count++;
      this.trace.push(['delivered', name, count]);
      // Content unique to this invocation; the copy is the driver's own baseline.
      const buffer = encoder.encode(`${name}#${instance}:${count}`);
      this.calls.push({ok: true, buffer, copy: Uint8Array.from(buffer)});
      return buffer;
    }});
    this.names.set(self, [name, instance]);
    if (!fresh) this.#shared.set(name, self);
    return self;
  }
  /** A present node's failure is recorded where it happens, by the failing reader. */
  refuser() {
    const error = new Error('refused');
    const self = Object.freeze({read: async () => {
      this.trace.push(['refused']);
      this.calls.push({ok: false, error});
      throw error;
    }});
    this.refusers.add(self);
    return self;
  }
  /** Constructs, then shows that neither the input nor the returned parts can change the tree. */
  construct(own, children) {
    const want = children.map(([key, child]) => [hex(key), child]);
    const input = children.map(([key, child]) => [Uint8Array.from(key), child]);
    const tree = this.T.compose(own, input);
    for (const pair of input) { pair[0].fill(0x7a); pair[1] = undefined; }
    input.length = 0;
    const same = got => got.length === want.length && want.every(([key, child]) => got.some(([k, c]) => hex(k) === key && c === child));
    const exact = () => {
      const parts = tree.decompose();
      return parts.own === own && tree.own() === own && same(parts.children) && same(tree.children());
    };
    let ok = exact();
    for (const returned of [tree.children(), tree.decompose().children]) {
      for (const pair of returned) {
        pair[0].fill(0x7a);
        try { pair[1] = undefined; } catch { /* A frozen pair protects the tree too. */ }
      }
      try { returned.length = 0; } catch { /* So does a frozen list. */ }
    }
    ok &&= exact();
    this.partsExact &&= ok;
    return tree;
  }
  /** An acyclic node implemented by the driver rather than the realization. */
  foreign(name, children) {
    const own = this.primitive(name);
    const node = {
      own: () => own,
      children: () => children.map(([key, child]) => [Uint8Array.from(key), child]),
      at: path => walk(node, path),
      decompose: () => ({own, children: node.children()}),
    };
    return node;
  }
  /** A driver-implemented node that contains itself. */
  loop() {
    const again = () => [[encoder.encode('again'), node]];
    const node = {own: () => this.refuser(), children: again, at: () => undefined, decompose: () => ({own: this.refuser(), children: again()})};
    return node;
  }
  build(id, fault = '', rootID = '', foreign = false) {
    const found = this.#built.get(id);
    if (found) return found;
    const d = this.declarations.get(id);
    const children = d.children.map(([key, child]) => [bytes(key), this.build(child, fault, rootID)]);
    if (id === rootID && fault === 'duplicate') children.push([encoder.encode('a'), this.build('leaf')]);
    if (id === rootID && fault === 'missingChild') children.push([encoder.encode('hole'), undefined]);
    if (id === rootID && fault === 'cycle') children.push([encoder.encode('loop'), this.loop()]);
    if (id === rootID && foreign) children.push([encoder.encode('foreign'), this.foreign('foreign', [[encoder.encode('inner'), this.foreign('inner', [])]])]);
    const tree = this.construct(d.own === null ? this.refuser() : this.primitive(d.own), children);
    this.#built.set(id, tree);
    return tree;
  }
  render(tree) {
    const own = tree.own();
    return {
      own: this.refusers.has(own) ? null : this.names.get(own) ?? 'unknown',
      children: tree.children().map(([key, child]) => [hex(key), this.render(child)]),
    };
  }
  rebuild(tree, where, keep) {
    if (keep.some(path => JSON.stringify(path) === JSON.stringify(where))) return tree;
    const parts = tree.decompose();
    return this.construct(parts.own, parts.children.map(([key, child]) => [key, this.rebuild(child, [...where, hex(key)], keep)]));
  }
  replaceAt(tree, path, f) {
    if (!path.length) return f(tree);
    const parts = tree.decompose();
    if (!parts.children.some(([key]) => hex(key) === path[0])) throw new Error('edit path leaves the tree');
    return this.construct(parts.own, parts.children.map(([key, child]) => [key, hex(key) === path[0] ? this.replaceAt(child, path.slice(1), f) : child]));
  }
  copy(tree) {
    const own = tree.own();
    const fresh = this.refusers.has(own) ? this.refuser() : this.primitive(this.named(own), true);
    return this.construct(fresh, tree.children().map(([key, child]) => [key, this.copy(child)]));
  }
  named(own) {
    const name = this.names.get(own)?.[0];
    if (name === undefined) throw new Error('an edit needs a named reader');
    return name;
  }
  edit(tree, s) {
    switch (s.op) {
      case 'rebuild': return this.rebuild(tree, [], s.keep);
      case 'replace': return this.replaceAt(tree, s.path, () => this.build(s.node));
      case 'own': return this.replaceAt(tree, s.path, node =>
        this.construct(s.own === null ? this.refuser() : this.primitive(this.named(node.own()), true), node.children()));
      case 'substitute': return this.replaceAt(tree, s.path, node => s.mode === 'copy' ? this.copy(node) : this.rebuild(node, [], []));
      case 'omit': case 'rename': case 'add': return this.replaceAt(tree, s.path, node => {
        const next = node.children()
          .filter(([key]) => !(s.op === 'omit' && hex(key) === s.key))
          .map(([key, child]) => [s.op === 'rename' && hex(key) === s.key ? bytes(s.to) : key, child]);
        if (s.op === 'add') next.push([bytes(s.key), this.build(s.node)]);
        return this.construct(node.own(), next);
      });
      default: throw new Error(`unknown edit ${s.op}`);
    }
  }
}

/**
 * Applies a selection chain with each node's own at. From the root, the chain
 * followed by path must select exactly what the concatenation selects, including
 * when a selection in the chain fails.
 */
function chain(h, start, selections, path, fromRoot) {
  let node = start;
  for (const selection of selections) {
    node = node.at(selection.map(bytes)) ?? undefined;
    if (!node) break;
  }
  if (fromRoot && h.T.select(start, [...selections.flat(), ...path].map(bytes)) !== (node && h.T.select(node, path.map(bytes)))) {
    h.trace.push(['selectionDiffers']);
  }
  return node;
}

/**
 * Derived reading. A present node's reader records its own success or failure;
 * what the caller sees must be exactly that. A missing path invokes no reader
 * and reports absence. Anything else is recorded as the violation it is.
 */
async function derived(h, base, path) {
  if (!base) { h.trace.push(['missing']); return; }
  const keys = path.map(bytes);
  const present = h.T.select(base, keys) !== undefined;
  h.calls = [];
  let outcome;
  try { outcome = {ok: true, value: await h.T.read(base, keys)}; } catch (error) { outcome = {ok: false, error}; }
  const calls = h.calls;
  h.calls = [];
  if (!present) {
    if (calls.length) h.trace.push(['fallback']);
    else if (outcome.ok) h.trace.push(['missingAdmitted']);
    else h.trace.push([isMissing(outcome.error) ? 'missing' : 'missingMisreported']);
    return;
  }
  if (calls.length !== 1) { h.trace.push([calls.length ? 'readMoreThanOnce' : outcome.ok ? 'readWithoutOwn' : 'refusedWithoutOwn']); return; }
  const [call] = calls;
  if (call.ok && !outcome.ok) { h.trace.push(['failedAfterRead']); return; }
  if (!call.ok && outcome.ok) { h.trace.push(['failureSwallowed']); return; }
  if (!call.ok) { h.unchanged &&= surfaces(outcome.error, call.error); return; }
  // The caller holds exactly this invocation's bytes, in a buffer the reader does not share.
  if (!(outcome.value instanceof Uint8Array)) { h.unchanged = false; return; }
  const exact = hex(outcome.value) === hex(call.copy);
  call.buffer.fill(0x7a);
  h.unchanged &&= exact && hex(outcome.value) === hex(call.copy);
}

async function local(T, inputs, test) {
  const h = new Harness(T, inputs);
  let root;
  try { root = h.build(test.root, test.fault, test.root, test.foreign); } catch { return {construction: 'refused'}; }
  if (test.fault) return {construction: 'accepted'};
  let view;
  for (const s of test.steps) {
    switch (s.op) {
      case 'structure': {
        const node = T.select(root, s.path.map(bytes));
        h.trace.push(['structure', node ? h.render(node) : 'missing']);
        break;
      }
      case 'send': {
        const start = s.via === 'view' ? view : root;
        await derived(h, start && chain(h, start, s.selections ?? [], s.path, s.via !== 'view'), s.path);
        break;
      }
      case 'same': {
        const [a, b] = s.paths.map(path => T.select(root, path.map(bytes))?.own());
        h.trace.push(['same', a !== undefined && a === b]);
        break;
      }
      case 'view': view = chain(h, root, s.selections, [], true); break;
      default: root = h.edit(root, s);
    }
  }
  return {trace: h.trace, partsExact: h.partsExact, unchanged: h.unchanged};
}

const [realization = '', inputPath] = process.argv.slice(2);
const T = realization === 'production' ? production : Object.hasOwn(mutants, realization) ? mutants[realization] : undefined;
if (!T || !inputPath) throw new Error(`Usage: trees.mjs production|${Object.keys(mutants).join('|')} inputs.json`);
const inputs = JSON.parse(readFileSync(inputPath, 'utf8'));
const output = [];
for (const test of inputs.cases) {
  if (test.family !== 'structure') continue;
  let observations;
  try {
    observations = await local(T, inputs, test);
  } catch (error) {
    if (T === production) throw error;
    observations = {failed: String(error)}; // A mutant may break the harness; that is a failed case too.
  }
  output.push({id: test.id, observations});
}
process.stdout.write(JSON.stringify(output) + '\n');
