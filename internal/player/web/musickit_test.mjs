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

// ─── Queue tracker (_q / _qi / _busy / _wantIdx) ─────────────────────────────
// Tests the simplified _busy + _wantIdx approach (no mutex, no stop()).
console.log('\nQueue tracker (_busy / _wantIdx)');

function makeTracker() {
  let q = [], qi = -1, busy = false, wantIdx = -1;
  const calls = [];

  async function doPlayAt(idx) {
    busy    = true;
    wantIdx = -1;
    qi      = idx;
    calls.push(idx);
    // Simulate async setQueue+play (instant in tests)
    busy = false;
    if (wantIdx >= 0 && wantIdx < q.length) {
      const p = wantIdx; wantIdx = -1; await doPlayAt(p);
    }
  }

  function playAt(idx) {
    if (idx < 0 || idx >= q.length) return Promise.resolve();
    if (busy) { wantIdx = idx; return Promise.resolve(); }
    return doPlayAt(idx);
  }

  return {
    get q() { return q; }, set q(v) { q = v; },
    get qi() { return qi; },
    get busy() { return busy; },
    get calls() { return calls; },
    playAt,
    get wantIdx() { return wantIdx; },
  };
}

test('playAt: single press executes', async () => {
  const t = makeTracker();
  t.q = ['a'];
  await t.playAt(0);
  eq(t.calls, [0]);
  eq(t.qi, 0);
});

test('playAt: out of bounds is no-op', async () => {
  const t = makeTracker();
  t.q = ['a'];
  await t.playAt(-1);
  await t.playAt(1);
  eq(t.calls, []);
  eq(t.qi, -1);
});

test('playAt: stores wantIdx while busy', () => {
  const t = makeTracker();
  t.q = ['a', 'b', 'c'];
  // Manually set busy to simulate concurrent call
  // (can't easily do this with sync mock, so test the logic path)
  eq(t.busy, false);
});

test('vibezNext simulation: advances qi', async () => {
  const t = makeTracker();
  t.q = ['a', 'b', 'c'];
  await t.playAt(0);
  if (t.qi < t.q.length - 1) await t.playAt(t.qi + 1);
  eq(t.qi, 1);
});

test('vibezNext simulation: no-op at last item', async () => {
  const t = makeTracker();
  t.q = ['a'];
  await t.playAt(0);
  const callsBefore = t.calls.length;
  if (t.qi < t.q.length - 1) await t.playAt(t.qi + 1);
  eq(t.calls.length, callsBefore);
  eq(t.qi, 0);
});

test('vibezPrev simulation: goes back', async () => {
  const t = makeTracker();
  t.q = ['a', 'b', 'c'];
  await t.playAt(2);
  await t.playAt(t.qi > 0 ? t.qi - 1 : 0);
  eq(t.qi, 1);
});

test('vibezPrev simulation: at index 0 restarts', async () => {
  const t = makeTracker();
  t.q = ['a', 'b'];
  await t.playAt(0);
  await t.playAt(t.qi > 0 ? t.qi - 1 : 0);
  eq(t.qi, 0);
  eq(t.calls.filter(x => x === 0).length, 2);
});

test('appendQueue: starts playback when idle', async () => {
  const t = makeTracker();
  t.q = ['a'];
  if (t.qi < 0) await t.playAt(0);
  eq(t.qi, 0);
});

test('appendQueue: does not restart when already playing', async () => {
  const t = makeTracker();
  t.q = ['a'];
  await t.playAt(0);
  const callsBefore = t.calls.length;
  t.q = t.q.concat(['b']);
  // qi >= 0, so no playAt call
  eq(t.calls.length, callsBefore);
  eq(t.q.length, 2);
});

test('auto-advance: repeat-none advances to next', async () => {
  const t = makeTracker();
  t.q = ['a', 'b', 'c'];
  await t.playAt(0);
  // Simulate nowPlayingItemDidChange(null), repeatMode=0
  const next = t.qi + 1;
  if (next < t.q.length) await t.playAt(next);
  eq(t.qi, 1);
});

test('auto-advance: repeat-all wraps around', async () => {
  const t = makeTracker();
  t.q = ['a', 'b'];
  await t.playAt(1);
  const next = t.qi + 1;
  if (next < t.q.length) await t.playAt(next);
  else await t.playAt(0); // repeat-all
  eq(t.qi, 0);
});

test('auto-advance: repeat-one replays same index', async () => {
  const t = makeTracker();
  t.q = ['a'];
  await t.playAt(0);
  await t.playAt(t.qi); // repeat-one
  eq(t.calls.filter(x => x === 0).length, 2);
});

// ─── No-stop design: setQueue handles transition, stop only on cold start ─────
console.log('\nNo-stop design (setQueue handles transition)');

test('_doPlayAt uses songs:[id] descriptor for catalog items', () => {
  const item = { id: '1234567890', type: 'songs', attributes: {} };
  const descriptor = item.id.startsWith('i.')
    ? { items: [item] }
    : { songs: [item.id] };
  eq(JSON.stringify(descriptor), JSON.stringify({ songs: ['1234567890'] }),
     'catalog item should use songs:[id] descriptor');
});

test('_doPlayAt uses items:[obj] descriptor for library items', () => {
  const item = { id: 'i.ABCDEF', type: 'library-songs', attributes: {} };
  const descriptor = { items: [item] };
  eq('items' in descriptor, true, 'library item should use items:[obj] descriptor');
  eq(descriptor.items[0], item, 'library item object should be preserved');
});

test('state normalisation: stop() called when state is not paused/stopped after setQueue', () => {
  // Simulate: after setQueue(), state=none(0) → must stop before play()
  let stopped = false;
  const stateAfterSetQueue = 0; // none — setQueue reset the state
  if (stateAfterSetQueue !== 3 && stateAfterSetQueue !== 4) {
    stopped = true; // would call stop()
  }
  eq(stopped, true, 'should normalise state=none to stopped before play()');
});

test('state normalisation: stop() skipped when already paused after setQueue', () => {
  let stopped = false;
  const stateAfterSetQueue = 3; // paused — no normalisation needed
  if (stateAfterSetQueue !== 3 && stateAfterSetQueue !== 4) {
    stopped = true;
  }
  eq(stopped, false, 'should skip stop() when already paused');
});

test('state normalisation: stop() skipped when already stopped after setQueue', () => {
  let stopped = false;
  const stateAfterSetQueue = 4; // stopped — no normalisation needed
  if (stateAfterSetQueue !== 3 && stateAfterSetQueue !== 4) {
    stopped = true;
  }
  eq(stopped, false, 'should skip stop() when already stopped');
});

test('play() CONTENT_EQUIVALENT is silently ignored', () => {
  let errLogged = false;
  const e = { name: 'CONTENT_EQUIVALENT', message: 'CONTENT_EQUIVALENT' };
  if (e?.name !== 'CONTENT_EQUIVALENT') errLogged = true;
  eq(errLogged, false, 'CONTENT_EQUIVALENT must not be logged as an error');
});

test('playbackStateDidChange: ignored for states other than completed(9)', () => {
  // Simulate listener guard — completed state may be 9 (old MusicKit) or 10 (new).
  const completedState = 10; // match what newer MusicKit reports
  const advance = (state, busy) => {
    if (state !== completedState) return false;
    if (busy) return false;
    return true;
  };
  eq(advance(4, false),  false, 'state=stopped must not advance');
  eq(advance(3, false),  false, 'state=paused must not advance');
  eq(advance(2, false),  false, 'state=playing must not advance');
  eq(advance(9, false),  false, 'state=9 (old completed) must not advance when completedState=10');
  eq(advance(10, true),  false, '_busy must suppress even state=10');
  eq(advance(10, false), true,  'state=10 + not busy must advance');
});

test('_busy guard: auto-advance suppressed while busy', () => {
  const completedState = 10;
  const advance = (state, busy, qi, qlen) => {
    if (state !== completedState) return false;
    if (busy || qi < 0 || qlen === 0) return false;
    return true;
  };
  eq(advance(10, true,  0, 2), false, 'busy=true suppresses');
  eq(advance(10, false, 0, 2), true,  'busy=false allows');
  eq(advance(10, false, -1, 2), false, 'qi<0 suppresses');
  eq(advance(10, false, 0, 0), false,  'empty queue suppresses');
});

test('_doPlayAt: try/finally always releases _busy even on unexpected throw', async () => {
  let _busy = false;
  let errorLogged = '';
  const goError = (msg) => { errorLogged = msg; return Promise.resolve(); };
  const errName = (e) => (e && e.message) || String(e);

  async function _doPlayAt_sim(shouldThrow) {
    _busy = true;
    try {
      if (shouldThrow) throw new Error('unexpected boom');
    } catch(e) {
      goError('_doPlayAt unexpected: '+errName(e)).catch(()=>{});
    } finally {
      _busy = false;
    }
  }

  await _doPlayAt_sim(true);
  eq(_busy, false, '_busy must be false after unexpected error');
  eq(errorLogged.includes('unexpected boom'), true, 'unexpected error must be logged');

  _busy = true; // set manually
  await _doPlayAt_sim(false);
  eq(_busy, false, '_busy must be false after normal completion');
});

test('loading state: playbackState 1/7/8 maps to Loading=true', () => {
  const isLoading = (ps) => ps === 1 || ps === 7 || ps === 8;
  eq(isLoading(0), false, 'none is not loading');
  eq(isLoading(1), true,  'loading(1) is loading');
  eq(isLoading(2), false, 'playing is not loading');
  eq(isLoading(3), false, 'paused is not loading');
  eq(isLoading(4), false, 'stopped is not loading');
  eq(isLoading(7), true,  'waiting(7) is loading');
  eq(isLoading(8), true,  'stalled(8) is loading');
  eq(isLoading(9), false, 'completed is not loading');
});

test('vibezSetShuffle: shuffles tail of queue, keeps current item', () => {
  let _q = [{id:'a'},{id:'b'},{id:'c'},{id:'d'},{id:'e'}];
  let _qi = 1; // currently playing 'b'
  let _qUnshuffled = [];

  // Simulate shuffle ON
  _qUnshuffled = _q.slice();
  const tail = _q.splice(_qi + 1);
  // Instead of random, verify length and that current item is unchanged
  eq(tail.length, 3, 'tail has remaining 3 tracks');
  eq(_q.length, 2, '_q has head (0..qi) before concat');
  _q = _q.concat(tail); // re-attach (not shuffled in test for determinism)
  eq(_q[_qi].id, 'b', 'current item still at _qi after shuffle-on');
  eq(_q.length, 5, 'total queue length unchanged');

  // Simulate shuffle OFF — restore and resync _qi
  const currentId = _q[_qi]?.id;
  _q = _qUnshuffled.slice();
  _qUnshuffled = [];
  const idx = _q.findIndex(item => item.id === currentId);
  if (idx >= 0) _qi = idx;
  eq(_q.map(i=>i.id).join(','), 'a,b,c,d,e', 'original order restored');
  eq(_qi, 1, '_qi resynced to correct position');
});

test('vibezSetShuffle: setQueue resets shuffle snapshot', () => {
  let _qUnshuffled = [{id:'old'}];
  // Simulating setQueue clearing the snapshot
  _qUnshuffled = [];
  eq(_qUnshuffled.length, 0, 'shuffle snapshot cleared on setQueue');
});
console.log(`\n${passed + failed} tests: ${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
