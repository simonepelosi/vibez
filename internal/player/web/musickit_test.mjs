#!/usr/bin/env node
// Pure-logic tests for musickit.html — no browser, no npm dependencies.
// Run: node internal/player/web/musickit_test.mjs
// Exit code 0 = all passed, 1 = failures.

// ─── Test harness ─────────────────────────────────────────────────────────────
let passed = 0, failed = 0;
const test = (name, fn) => {
  try { fn(); console.log(`  ✓ ${name}`); passed++; }
  catch (e) { console.error(`  ✗ ${name}: ${e.message}`); failed++; }
};
const eq = (a, b, msg) => {
  if (JSON.stringify(a) !== JSON.stringify(b))
    throw new Error(`${msg ?? ''} expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}`);
};

// ─── Functions copied verbatim from musickit.html ────────────────────────────
// Keep these in sync: if the originals change, update here too and re-run.

const errName = (e) => {
  if (!e) return '';
  const name = (typeof e.name === 'string' && e.name) ? e.name : '';
  const msg  = (typeof e.message === 'string' && e.message) ? e.message : '';
  if (name && msg && name !== 'Error') return name + ': ' + msg;
  return msg || name || String(e);
};

// ID routing helpers (inlined from vibezSetQueue / vibezAppendQueue).
const isLibraryID = (id) => id.startsWith('i.');
const isCatalogID = (id) => !isLibraryID(id);
const partitionIDs = (allIds) => ({
  catalog: allIds.filter(isCatalogID),
  library: allIds.filter(isLibraryID),
});

// ─── errName ─────────────────────────────────────────────────────────────────
console.log('\nerrName()');
test('null → empty string', () => eq(errName(null), ''));
test('undefined → empty string', () => eq(errName(undefined), ''));
test('TypeError with message', () =>
  eq(errName(new TypeError('e.map is not a function')), 'TypeError: e.map is not a function'));
test('plain Error → message only (no "Error:" prefix)', () =>
  eq(errName(new Error('something went wrong')), 'something went wrong'));
test('RangeError with message', () =>
  eq(errName(new RangeError('out of range')), 'RangeError: out of range'));
test('string error → returned as-is', () =>
  eq(errName('raw string error'), 'raw string error'));
test('object with only name', () =>
  eq(errName({ name: 'CustomError', message: '' }), 'CustomError'));
test('object with only message', () =>
  eq(errName({ name: '', message: 'only message' }), 'only message'));
test('CONTENT_EQUIVALENT (known MusicKit code)', () =>
  eq(errName({ name: 'CONTENT_EQUIVALENT', message: '' }), 'CONTENT_EQUIVALENT'));

// ─── ID routing ──────────────────────────────────────────────────────────────
console.log('\nID routing (catalog vs library)');
test('catalog numeric ID', () => eq(isLibraryID('1234567890'), false));
test('library i. prefix', () => eq(isLibraryID('i.AbCdEf123'), true));
test('empty string → not a library ID', () => eq(isLibraryID(''), false));
test('isCatalogID numeric', () => eq(isCatalogID('987654321'), true));
test('isCatalogID library → false', () => eq(isCatalogID('i.XyZ'), false));

console.log('\npartitionIDs()');
test('mixed → correct split', () => {
  const r = partitionIDs(['123', 'i.abc', '456', 'i.def']);
  eq(r.catalog, ['123', '456']);
  eq(r.library, ['i.abc', 'i.def']);
});
test('all catalog', () => {
  const r = partitionIDs(['1', '2', '3']);
  eq(r.catalog, ['1', '2', '3']);
  eq(r.library, []);
});
test('all library', () => {
  const r = partitionIDs(['i.a', 'i.b']);
  eq(r.catalog, []);
  eq(r.library, ['i.a', 'i.b']);
});
test('empty array', () => {
  const r = partitionIDs([]);
  eq(r.catalog, []);
  eq(r.library, []);
});
test('single catalog ID preserved', () => {
  const r = partitionIDs(['1622205917']);
  eq(r.catalog, ['1622205917']);
});
test('single library ID preserved', () => {
  const r = partitionIDs(['i.Abcdef123456']);
  eq(r.library, ['i.Abcdef123456']);
});
test('order preserved within each partition', () => {
  const r = partitionIDs(['111', 'i.aaa', '222', 'i.bbb', '333']);
  eq(r.catalog, ['111', '222', '333']);
  eq(r.library, ['i.aaa', 'i.bbb']);
});

// ─── Summary ─────────────────────────────────────────────────────────────────
console.log(`\n${passed + failed} tests: ${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
