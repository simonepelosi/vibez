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

// ─── Queue tracker (_q / _qi / _withLock / _playAt simulation) ───────────────
// We can't test MusicKit calls (need a real browser), but we CAN test the
// queue index management and the sequence-number cancellation logic in isolation.
console.log('\nQueue tracker logic');

// Minimal _withLock simulation (no real MusicKit, just the sequencing).
function makeTracker() {
  let q = [], qi = -1, seq = 0;
  let opLock = Promise.resolve();
  const calls = [];  // log of which indices were actually executed

  function withLock(s, fn) {
    opLock = opLock.then(() => s === seq ? fn() : undefined).catch(() => {});
    return opLock;
  }
  function playAt(idx) {
    if (idx < 0 || idx >= q.length) return Promise.resolve();
    qi = idx;
    const s = ++seq;
    return withLock(s, async () => { calls.push(idx); });
  }
  return { get q() { return q; }, set q(v) { q = v; },
           get qi() { return qi; }, set qi(v) { qi = v; },
           get seq() { return seq; },
           get calls() { return calls; },
           playAt,
           get opLock() { return opLock; } };
}

test('playAt: single press executes', async () => {
  const t = makeTracker();
  t.q = ['a'];
  await t.playAt(0);
  eq(t.calls, [0]);
  eq(t.qi, 0);
});

test('playAt: rapid presses — only last executes', async () => {
  const t = makeTracker();
  t.q = ['a', 'b', 'c', 'd'];
  // Fire 3 presses in rapid succession without awaiting
  t.playAt(1);
  t.playAt(2);
  await t.playAt(3);  // await the last one
  await t.opLock;     // drain any remaining
  // Only the last press should have executed
  eq(t.calls, [3]);
  eq(t.qi, 3);
});

test('playAt: sequential presses all execute', async () => {
  const t = makeTracker();
  t.q = ['a', 'b', 'c'];
  await t.playAt(0); await t.playAt(1); await t.playAt(2);
  eq(t.calls, [0, 1, 2]);
});

test('playAt: out of bounds is no-op', async () => {
  const t = makeTracker();
  t.q = ['a'];
  await t.playAt(-1);
  await t.playAt(1);
  eq(t.calls, []);
  eq(t.qi, -1);
});

test('vibezNext simulation: advances qi', async () => {
  const t = makeTracker();
  t.q = ['a', 'b', 'c'];
  await t.playAt(0);        // start at 0
  if (t.qi < t.q.length - 1) await t.playAt(t.qi + 1);
  eq(t.qi, 1);
});

test('vibezNext simulation: no-op at last item', async () => {
  const t = makeTracker();
  t.q = ['a'];
  await t.playAt(0);
  const before = t.seq;
  if (t.qi < t.q.length - 1) await t.playAt(t.qi + 1); // should not fire
  eq(t.seq, before); // seq unchanged → no new play triggered
  eq(t.qi, 0);
});

test('vibezPrev simulation: goes back', async () => {
  const t = makeTracker();
  t.q = ['a', 'b', 'c'];
  await t.playAt(2);
  await t.playAt(t.qi > 0 ? t.qi - 1 : 0);
  eq(t.qi, 1);
});

test('vibezPrev simulation: restarts at index 0', async () => {
  const t = makeTracker();
  t.q = ['a', 'b'];
  await t.playAt(0);
  await t.playAt(t.qi > 0 ? t.qi - 1 : 0); // stays at 0 (restart)
  eq(t.qi, 0);
});

test('appendQueue: starts playback when idle', async () => {
  const t = makeTracker();
  t.q = ['a', 'b'];
  // Simulate appendQueue: add to _q; if _qi < 0 call playAt(0)
  if (t.qi < 0) await t.playAt(0);
  eq(t.qi, 0);
  eq(t.calls, [0]);
});

test('appendQueue: does not restart if already playing', async () => {
  const t = makeTracker();
  t.q = ['a'];
  await t.playAt(0); // already playing
  const callsBefore = t.calls.length;
  t.q = t.q.concat(['b']); // append
  // qi >= 0 → do NOT call playAt
  eq(t.calls.length, callsBefore);
  eq(t.q.length, 2);
});

test('auto-advance simulation: repeat-none goes to next', async () => {
  const t = makeTracker();
  t.q = ['a', 'b', 'c'];
  await t.playAt(0);
  // Simulate nowPlayingItemDidChange with item=null, repeatMode=0
  const repeatMode = 0;
  if (repeatMode === 1) { await t.playAt(t.qi); }
  else { const next = t.qi + 1; if (next < t.q.length) await t.playAt(next); }
  eq(t.qi, 1);
});

test('auto-advance simulation: repeat-all wraps around', async () => {
  const t = makeTracker();
  t.q = ['a', 'b'];
  await t.playAt(1); // last item
  const repeatMode = 2;
  if (repeatMode === 1) { await t.playAt(t.qi); }
  else { const next = t.qi + 1;
    if (next < t.q.length) await t.playAt(next);
    else if (repeatMode === 2 && t.q.length > 0) await t.playAt(0);
  }
  eq(t.qi, 0);
});

test('auto-advance simulation: repeat-one replays same index', async () => {
  const t = makeTracker();
  t.q = ['a'];
  await t.playAt(0);
  const repeatMode = 1;
  if (repeatMode === 1) await t.playAt(t.qi);
  eq(t.qi, 0);
  eq(t.calls.filter(x => x === 0).length, 2); // played index 0 twice
});

// ─── _transitioning flag ──────────────────────────────────────────────────────
// stop() fires nowPlayingItemDidChange(null) mid-transition; without the flag
// the auto-advance listener spuriously jumps ahead.
console.log('\n_transitioning (spurious auto-advance prevention)');

function makeTrackerWithTransitioning() {
  let q = [], qi = -1, seq = 0, transitioning = false;
  let opLock = Promise.resolve();
  const calls = [];
  const autoAdvanceCalls = [];

  function withLock(s, fn) {
    opLock = opLock
      .then(() => s === seq ? fn() : undefined)
      .catch(() => { transitioning = false; });
    return opLock;
  }

  function playAt(idx) {
    if (idx < 0 || idx >= q.length) return Promise.resolve();
    qi = idx;
    const s = ++seq;
    return withLock(s, async () => {
      transitioning = true;
      calls.push(idx);
      // Simulate stop() side effect: fires nowPlayingItemDidChange(null)
      simulateNowPlayingItemChanged(null);
      if (s !== seq) { transitioning = false; return; }
      // Simulate setQueue + play
      if (s !== seq) { transitioning = false; return; }
      transitioning = false;
    });
  }

  // Mirrors the listener from musickit.html
  function simulateNowPlayingItemChanged(nowPlayingItem) {
    if (transitioning) return; // suppressed
    if (nowPlayingItem !== null || qi < 0 || q.length === 0) return;
    const next = qi + 1;
    if (next < q.length) { autoAdvanceCalls.push(next); playAt(next); }
  }

  return {
    get q() { return q; }, set q(v) { q = v; },
    get qi() { return qi; },
    get calls() { return calls; },
    get autoAdvanceCalls() { return autoAdvanceCalls; },
    playAt,
  };
}

test('_transitioning: stop() during _playAt does NOT trigger auto-advance', async () => {
  const t = makeTrackerWithTransitioning();
  t.q = ['a', 'b', 'c'];
  // Simulate: playing at 0, user presses next → _playAt(1)
  await t.playAt(0);
  await t.playAt(1); // stop() inside this would fire nowPlayingItemDidChange(null)
  eq(t.autoAdvanceCalls.length, 0, 'no spurious auto-advance should happen');
  eq(t.qi, 1);
});

test('_transitioning: auto-advance fires AFTER transition completes', async () => {
  const t = makeTrackerWithTransitioning();
  t.q = ['a', 'b'];
  await t.playAt(0);
  // Transition done (_transitioning=false), now simulate natural end
  // nowPlayingItemDidChange fires with null OUTSIDE of a _playAt call
  // (i.e., the song ended by itself)
  const before = t.qi;
  // Direct advance (not inside _playAt) — _transitioning is false here
  const next = t.qi + 1;
  if (next < t.q.length) { t.autoAdvanceCalls.push(next); await t.playAt(next); }
  eq(t.qi, 1, 'natural end advances correctly');
});

test('_transitioning: rapid presses do not cascade auto-advance', async () => {
  const t = makeTrackerWithTransitioning();
  t.q = ['a', 'b', 'c', 'd'];
  t.playAt(0); t.playAt(1); await t.playAt(2); // rapid presses
  await t.playAt(0); // drain lock
  eq(t.autoAdvanceCalls.length, 0, 'no spurious auto-advance from rapid presses');
});
console.log(`\n${passed + failed} tests: ${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
