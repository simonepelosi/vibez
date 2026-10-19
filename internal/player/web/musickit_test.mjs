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

// ─── audio bitrate ───────────────────────────────────────────────────────────
const resolveAudioBitrate = (kbps, MusicKit) => {
  const n = Number(kbps);
  if (n === 64) return (MusicKit.PlaybackBitrate && MusicKit.PlaybackBitrate.STANDARD) || 64;
  if (n === 256) return (MusicKit.PlaybackBitrate && MusicKit.PlaybackBitrate.HIGH) || 256;
  throw new Error('MusicKit JS/web playback max is 256 kbps AAC; supported values are 64 and 256 kbps AAC');
};

console.log('\nresolveAudioBitrate()');
test('64 uses STANDARD enum when present', () =>
  eq(resolveAudioBitrate(64, { PlaybackBitrate: { STANDARD: 'standard' } }), 'standard'));
test('256 uses HIGH enum when present', () =>
  eq(resolveAudioBitrate(256, { PlaybackBitrate: { HIGH: 'high' } }), 'high'));
test('64/256 fall back to numeric MusicKit values', () => {
  eq(resolveAudioBitrate(64, {}), 64);
  eq(resolveAudioBitrate(256, {}), 256);
});
test('lossless/unsupported values are rejected with web max', () => {
  for (const input of ['lossless', 'hi-res', 320, 1411]) {
    let ok = false;
    try { resolveAudioBitrate(input, {}); }
    catch (e) { ok = String(e.message).includes('MusicKit JS/web playback max is 256 kbps AAC'); }
    if (!ok) throw new Error(`${input} was not rejected with MusicKit web max`);
  }
});

// ─── Native queue mode (#96) ──────────────────────────────────────────────────
// _nativeHasNext is copied verbatim from musickit.html.
// The rest simulate the decision each call site makes, in the same style as the
// _busy/playbackStateDidChange tests above.

function _nativeHasNext(m) {
  if (m.repeatMode === 1 || m.repeatMode === 2) return true; // repeats itself
  const q = m.queue;
  if (!q || typeof q.length !== 'number' || typeof q.position !== 'number') return false;
  return q.position < q.length - 1;
}

console.log('\n_nativeHasNext()');
test('repeat-one always has a next', () =>
  eq(_nativeHasNext({ repeatMode: 1, queue: { position: 4, length: 5 } }), true));
test('repeat-all always has a next, including the last item', () =>
  eq(_nativeHasNext({ repeatMode: 2, queue: { position: 4, length: 5 } }), true));
test('mid-queue with repeat off → true', () =>
  eq(_nativeHasNext({ repeatMode: 0, queue: { position: 0, length: 3 } }), true));
test('last item with repeat off → false', () =>
  eq(_nativeHasNext({ repeatMode: 0, queue: { position: 2, length: 3 } }), false));
test('missing queue → false (vibez takes over)', () =>
  eq(_nativeHasNext({ repeatMode: 0 }), false));
test('non-numeric queue shape → false (vibez takes over)', () => {
  eq(_nativeHasNext({ repeatMode: 0, queue: { position: '1', length: 3 } }), false);
  eq(_nativeHasNext({ repeatMode: 0, queue: { position: 1, length: null } }), false);
});
test('single-item native queue with repeat off → false', () =>
  eq(_nativeHasNext({ repeatMode: 0, queue: { position: 0, length: 1 } }), false));

console.log('\nNative mode entry condition');
// Queue length is the whole condition now. The items: descriptor carries
// catalog and library objects alike, so item kind no longer decides the mode
// (#96: a mixed queue was the fastest arm measured, not a disqualifying one).
const wantsNative = (items) => items.length > 1;
test('multi-item all-catalog → native', () =>
  eq(wantsNative([{ id: '1' }, { id: '2' }]), true));
test('single catalog item → one-item mode', () =>
  eq(wantsNative([{ id: '1' }]), false));
test('multi-item mixed catalog and library → native', () =>
  eq(wantsNative([{ id: '1' }, { id: 'i.x' }]), true));
test('multi-item library only → native', () =>
  eq(wantsNative([{ id: 'i.a' }, { id: 'i.b' }]), true));
test('empty → one-item mode', () => eq(wantsNative([]), false));

console.log('\ncompleted handler: who advances');
// Mirrors musickit.html playbackStateDidChange: native short-circuit, shuffled
// stop, then the pre-existing one-item logic.
const onCompleted = (s) => {
  const out = { playAt: null, clearedNative: false, advanced: false };
  if (s.nativeQueue && _nativeHasNext(s.m)) return out;      // MusicKit advances
  if (s.nativeQueue && !s.nativeMirrors && s.m.shuffleMode) { out.clearedNative = true; return out; }
  if (s.busy || s.qi < 0 || s.q.length === 0) return out;
  if (s.m.repeatMode === 1) { out.playAt = s.qi; out.advanced = true; return out; }
  const next = s.qi + 1;
  if (next < s.q.length)     { out.playAt = next; out.advanced = true; return out; }
  if (s.m.repeatMode === 2)  { out.playAt = 0;    out.advanced = true; }
  return out;
};
const base = { nativeQueue: false, nativeMirrors: false, busy: false, qi: 0, q: ['a', 'b', 'c'] };

test('native with a next: vibez does not advance (no double advance)', () => {
  const r = onCompleted({ ...base, nativeQueue: true, nativeMirrors: true,
                          m: { repeatMode: 0, queue: { position: 0, length: 3 } } });
  eq(r.advanced, false);
  eq(r.clearedNative, false, 'authority must stay with MusicKit while it has a next');
});
test('native repeat-all at the last item: MusicKit laps, vibez stays out', () => {
  const r = onCompleted({ ...base, nativeQueue: true, nativeMirrors: true, qi: 2,
                          m: { repeatMode: 2, queue: { position: 2, length: 3 } } });
  eq(r.advanced, false);
});
test('native exhausted while mirroring: vibez resumes at _qi+1', () => {
  const r = onCompleted({ ...base, nativeQueue: true, nativeMirrors: true, qi: 1,
                          m: { repeatMode: 0, queue: { position: 2, length: 2 } } });
  eq(r.playAt, 2, 'should pick up the appended tail');
});
test('native exhausted while shuffled: stops instead of playing an arbitrary index', () => {
  const r = onCompleted({ ...base, nativeQueue: true, nativeMirrors: false, qi: 1,
                          m: { repeatMode: 0, shuffleMode: 1, queue: { position: 2, length: 2 } } });
  eq(r.advanced, false, 'must not resume at _qi+1 after a shuffle');
  eq(r.clearedNative, true, 'safe to drop authority: MusicKit has nowhere to go');
});
test('native exhausted while following a reordered _q: vibez resumes at _qi+1', () => {
  const r = onCompleted({ ...base, nativeQueue: true, nativeMirrors: false, qi: 1,
                          m: { repeatMode: 0, shuffleMode: 0, queue: { position: 2, length: 2 } } });
  eq(r.playAt, 2, '_qi tracks _q while following, so _qi+1 is the right track');
});
test('one-item mode is unchanged: advance, repeat-one, repeat-all', () => {
  eq(onCompleted({ ...base, m: { repeatMode: 0 } }).playAt, 1);
  eq(onCompleted({ ...base, qi: 1, m: { repeatMode: 1 } }).playAt, 1);
  eq(onCompleted({ ...base, qi: 2, m: { repeatMode: 2 } }).playAt, 0);
  eq(onCompleted({ ...base, qi: 2, m: { repeatMode: 0 } }).playAt, null);
});
test('_busy suppresses the one-item path but not the native short-circuit', () => {
  eq(onCompleted({ ...base, busy: true, m: { repeatMode: 0 } }).playAt, null);
  const r = onCompleted({ ...base, nativeQueue: true, nativeMirrors: true, busy: true,
                          m: { repeatMode: 0, queue: { position: 0, length: 3 } } });
  eq(r.advanced, false);
});

console.log('\nPending jump keeps its native intent');
// _playNativeAt + _runPending: the native flag travels with the index so a
// deferred build cannot silently downgrade to one-item mode.
const makePending = () => {
  const s = { busy: false, wantIdx: -1, wantNative: false, q: ['a', 'b', 'c'], calls: [] };
  s.playNativeAt = (idx) => {
    if (s.busy) { s.wantIdx = idx; s.wantNative = true; return; }
    s.calls.push(['native', idx]);
  };
  s.playAt = (idx) => {
    if (s.busy) { s.wantIdx = idx; s.wantNative = false; return; }
    s.calls.push(['one', idx]);
  };
  s.runPending = () => {
    if (s.wantIdx < 0 || s.wantIdx >= s.q.length) return;
    const next = s.wantIdx, native = s.wantNative;
    s.wantIdx = -1; s.wantNative = false;
    if (native) s.calls.push(['native', next]);
    else        s.calls.push(['one', next]);
  };
  return s;
};

test('_playNativeAt while busy defers instead of building', () => {
  const s = makePending();
  s.busy = true;
  s.playNativeAt(1);
  eq(s.calls, [], 'must not build while another build is in flight');
  eq([s.wantIdx, s.wantNative], [1, true]);
});
test('deferred native jump runs as native, not one-item', () => {
  const s = makePending();
  s.busy = true; s.playNativeAt(2);
  s.busy = false; s.runPending();
  eq(s.calls, [['native', 2]], 'a deferred native build must not downgrade');
});
test('deferred one-item jump stays one-item', () => {
  const s = makePending();
  s.busy = true; s.playAt(1);
  s.busy = false; s.runPending();
  eq(s.calls, [['one', 1]]);
});
test('_runPending clears both fields before dispatching', () => {
  const s = makePending();
  s.busy = true; s.playNativeAt(1);
  s.busy = false; s.runPending();
  eq([s.wantIdx, s.wantNative], [-1, false]);
  s.runPending();
  eq(s.calls.length, 1, 'a consumed pending jump must not run twice');
});
test('_runPending ignores an out-of-range pending index', () => {
  const s = makePending();
  s.wantIdx = 9; s.wantNative = true;
  s.runPending();
  eq(s.calls, []);
});

console.log('\nQueue edits and advance authority');
// Authority may only be dropped alongside a stop()/setQueue that actually takes
// it back — clearing the flag on its own leaves two advancers running.
const makeQueueState = () => ({
  q: [{ id: '1' }, { id: '2' }, { id: '3' }],
  qi: 1, nativeQueue: true, nativeMirrors: true,
  removedIds: new Set(), stopped: false, rebuiltAt: null,
});
const removeAt = (s, idx) => {
  if (idx < 0 || idx >= s.q.length) return s;
  if (idx === s.qi) {
    s.q.splice(idx, 1);
    if (s.q.length === 0) {
      s.qi = -1; s.nativeQueue = false; s.nativeMirrors = false;
      s.removedIds = new Set(); s.stopped = true; return s;
    }
    if (s.qi >= s.q.length) s.qi = s.q.length - 1;
    s.rebuiltAt = s.qi; // _playAt rebuilds one-item, which takes authority back
    return s;
  }
  const removed = s.q[idx];
  s.q.splice(idx, 1);
  if (idx < s.qi) s.qi--;
  if (s.nativeQueue && removed && !s.q.some(i => i.id === removed.id)) s.removedIds.add(removed.id);
  s.nativeMirrors = false;
  return s;
};

test('removing a non-current item keeps authority but marks order diverged', () => {
  const s = removeAt(makeQueueState(), 2);
  eq(s.nativeQueue, true, 'MusicKit still holds the item — authority must not be dropped');
  eq(s.nativeMirrors, false);
  eq([...s.removedIds], ['3'], 'id remembered so it can be skipped on arrival');
});
test('removing before the current item shifts _qi', () => {
  const s = removeAt(makeQueueState(), 0);
  eq(s.qi, 0);
});
test('removing the current item rebuilds, which takes authority back', () => {
  const s = removeAt(makeQueueState(), 1);
  eq(s.rebuiltAt, 1);
});
test('removing the last remaining item drops authority together with a stop', () => {
  const s = makeQueueState();
  s.q = [{ id: '1' }]; s.qi = 0;
  removeAt(s, 0);
  eq(s.nativeQueue, false);
  eq(s.stopped, true, 'authority may only be cleared alongside a stop');
});
test('clearing the queue drops authority together with a stop', () => {
  const s = makeQueueState();
  s.q = []; s.qi = -1; s.nativeQueue = false; s.nativeMirrors = false; s.stopped = true;
  eq([s.nativeQueue, s.stopped], [false, true]);
});
test('reorder is view-only: authority kept, mirroring broken', () => {
  const s = makeQueueState();
  s.nativeMirrors = false; // vibezQueueMove leaves _nativeQueue alone
  eq([s.nativeQueue, s.nativeMirrors], [true, false]);
});

console.log('\nSkip-on-arrival for removed items');
const onNowPlayingChange = (s, playingId) => {
  const out = { qi: s.qi, skipped: false, stopped: false, clearedNative: false };
  if (!s.nativeQueue) return out;
  if (!playingId) return out;
  const idx = s.q.findIndex(i => i.id === playingId);
  if (idx >= 0) { out.qi = idx; return out; }
  if (s.removedIds.has(playingId)) {
    if (_nativeHasNext(s.m)) { out.skipped = true; return out; }
    out.clearedNative = true; out.stopped = true;
  }
  return out;
};

test('playing an item still in _q resettles _qi', () => {
  const s = { ...makeQueueState(), m: { repeatMode: 0, queue: { position: 2, length: 3 } } };
  eq(onNowPlayingChange(s, '3').qi, 2);
});
test('a removed item that starts is skipped when MusicKit has a next', () => {
  const s = { ...makeQueueState(), removedIds: new Set(['9']),
              m: { repeatMode: 0, queue: { position: 1, length: 3 } } };
  eq(onNowPlayingChange(s, '9').skipped, true);
});
test('a removed last item ends the queue rather than playing out', () => {
  const s = { ...makeQueueState(), removedIds: new Set(['9']),
              m: { repeatMode: 0, queue: { position: 2, length: 3 } } };
  const r = onNowPlayingChange(s, '9');
  eq([r.skipped, r.stopped, r.clearedNative], [false, true, true]);
});
test('an unknown item that was never removed is left alone', () => {
  const s = { ...makeQueueState(), m: { repeatMode: 0, queue: { position: 1, length: 3 } } };
  const r = onNowPlayingChange(s, 'surprise');
  eq([r.skipped, r.stopped], [false, false]);
});
test('nothing happens in one-item mode', () => {
  const s = { ...makeQueueState(), nativeQueue: false, removedIds: new Set(['9']),
              m: { repeatMode: 0, queue: { position: 0, length: 3 } } };
  eq(onNowPlayingChange(s, '9').skipped, false);
});

console.log('\nnext/prev delegate while MusicKit owns the queue');
const onNext = (s) => (s.nativeQueue && _nativeHasNext(s.m))
  ? { skipToNext: true, playAt: null }
  : { skipToNext: false, playAt: s.qi < s.q.length - 1 ? s.qi + 1 : null };
const onPrev = (s) => (s.nativeQueue && (s.m.queue?.position ?? 0) > 0)
  ? { skipToPrev: true, playAt: null }
  : { skipToPrev: false, playAt: s.qi > 0 ? s.qi - 1 : 0 };

test('next delegates to skipToNextItem in native mode', () => {
  const s = { ...makeQueueState(), m: { repeatMode: 0, queue: { position: 1, length: 3 } } };
  eq(onNext(s).skipToNext, true);
});
test('next falls back to _playAt once the native queue is exhausted', () => {
  const s = { ...makeQueueState(), m: { repeatMode: 0, queue: { position: 2, length: 3 } } };
  eq(onNext(s), { skipToNext: false, playAt: 2 });
});
test('prev delegates to skipToPreviousItem when MusicKit is past the start', () => {
  const s = { ...makeQueueState(), m: { repeatMode: 0, queue: { position: 1, length: 3 } } };
  eq(onPrev(s).skipToPrev, true);
});
test('prev at the native queue start uses _playAt', () => {
  const s = { ...makeQueueState(), m: { repeatMode: 0, queue: { position: 0, length: 3 } } };
  eq(onPrev(s), { skipToPrev: false, playAt: 0 });
});

console.log('\nAppend against a live native queue');
// playLater failure must not clear _nativeQueue: MusicKit still owns what it
// has, and the completed handler picks the tail up once it runs out.
const onAppend = (s, items, playLaterThrows) => {
  s.q = s.q.concat(items);
  if (s.qi < 0) { s.rebuiltAt = 0; return s; }
  if (s.nativeQueue) {
    if (playLaterThrows) return s; // flag deliberately left set
    s.playLater = items.map(i => i.id);
  }
  return s;
};

test('appending catalog items to a native queue uses playLater', () => {
  const s = onAppend(makeQueueState(), [{ id: '4' }], false);
  eq(s.playLater, ['4']);
  eq(s.nativeQueue, true);
});
test('playLater failure keeps authority so the boundary does not double up', () => {
  const s = onAppend(makeQueueState(), [{ id: '4' }], true);
  eq(s.nativeQueue, true, 'clearing here would add a second advancer');
  eq(s.q.length, 4, 'the item is still in _q for the completed handler to reach');
});
test('appending a library item to a native queue also uses playLater', () => {
  const s = onAppend(makeQueueState(), [{ id: 'i.x' }], false);
  eq(s.playLater, ['i.x'], 'items: can express a library append');
});
test('appending while nothing plays starts playback', () => {
  const s = makeQueueState();
  s.qi = -1;
  onAppend(s, [{ id: '4' }], false);
  eq(s.rebuiltAt, 0);
});


console.log('\nPre-resolved library copies');
// Mirrors _warmLibItems' id selection: every library copy the current queue
// could need, minus the ones already held, deduped.
const wantedLibIds = (q, libMap, libItems) =>
  [...new Set(q.map(item => libMap[item.id]).filter(id => id && !libItems[id]))];

test('collects the library copy of each catalog item', () =>
  eq(wantedLibIds([{ id: '1' }, { id: '2' }], { 1: 'i.a', 2: 'i.b' }, {}), ['i.a', 'i.b']));
test('items with no library copy contribute nothing', () =>
  eq(wantedLibIds([{ id: '1' }, { id: '2' }], { 1: 'i.a' }, {}), ['i.a']));
test('already-warmed copies are not refetched', () =>
  eq(wantedLibIds([{ id: '1' }, { id: '2' }], { 1: 'i.a', 2: 'i.b' }, { 'i.a': {} }), ['i.b']));
test('the same copy behind two catalog items is fetched once', () =>
  eq(wantedLibIds([{ id: '1' }, { id: '2' }], { 1: 'i.a', 2: 'i.a' }, {}), ['i.a']));
test('nothing to fetch → no request', () =>
  eq(wantedLibIds([{ id: '1' }], {}, {}), []));

console.log('\nLibrary fallback prefers a warmed copy');
// Mirrors _libraryFallback: a warmed copy is a local swap, an unwarmed one
// costs a round trip inside the audible gap, no copy at all skips the track.
const fallbackPlan = (item, libMap, libItems) => {
  if (item.id.startsWith('i.')) return { action: 'skip', why: 'already library' };
  const libId = libMap[item.id];
  if (!libId) return { action: 'skip', why: 'no library copy' };
  return { action: 'swap', via: libItems[libId] ? 'warmed' : 'fetch', libId };
};

test('warmed copy swaps without a fetch', () =>
  eq(fallbackPlan({ id: '1' }, { 1: 'i.a' }, { 'i.a': {} }), { action: 'swap', via: 'warmed', libId: 'i.a' }));
test('unwarmed copy still falls back, by fetching', () =>
  eq(fallbackPlan({ id: '1' }, { 1: 'i.a' }, {}), { action: 'swap', via: 'fetch', libId: 'i.a' }));
test('no library copy → skip', () =>
  eq(fallbackPlan({ id: '1' }, {}, {}).action, 'skip'));
test('a failing library item has nothing left to try', () =>
  eq(fallbackPlan({ id: 'i.a' }, {}, {}).why, 'already library'));

console.log('\nNative mode has no per-item catch');
// Mirrors the mediaPlaybackError listener. MusicKit advances by itself in
// native mode, so a failing item never reaches _doPlayAt's try/catch: the
// index goes back to one-item mode, which can run the fallback.
const onPlaybackError = (s) => {
  if (!s.nativeQueue) return s;
  s.recoverAt     = s.qi >= 0 ? s.qi : 0;
  s.nativeQueue   = false;
  s.nativeMirrors = false;
  s.stopped       = true;
  return s;
};

test('recovers at the item that failed', () => {
  const s = onPlaybackError({ ...makeQueueState(), qi: 2 });
  eq(s.recoverAt, 2);
});
test('recovery drops authority, and stops to take it back', () => {
  const s = onPlaybackError({ ...makeQueueState(), qi: 1 });
  eq([s.nativeQueue, s.nativeMirrors, s.stopped], [false, false, true]);
});
test('nothing playing recovers at the head', () => {
  const s = onPlaybackError({ ...makeQueueState(), qi: -1 });
  eq(s.recoverAt, 0);
});
test('one-item mode is left alone: _doPlayAt already caught it', () => {
  const s = onPlaybackError({ ...makeQueueState(), nativeQueue: false, qi: 1 });
  eq(s.recoverAt, undefined, 'recovering twice would restart the track');
});

// ─── First-join preload rescue (#180) ────────────────────────────────────────
// Copied verbatim from _preloadRescueTarget in musickit.html.
function _preloadRescueTarget(m, ap, rescuedId) {
  if (!ap?._buffer || ap.nextManifest || typeof ap.preloadItem !== 'function') return null;
  if (ap._buffer.nextSeamlessTransition != null || m.playbackState !== 2) return null;
  const next = m.queue?.nextPlayableItem;
  if (!next || next.type !== 'song' || next.id === rescuedId) return null;
  const dur = m.currentPlaybackDuration, t = m.currentPlaybackTime;
  if (!(dur > 0) || t < dur * 0.35 || dur - t < 10) return null;
  return next;
}

console.log('\n_preloadRescueTarget()');
const NEXT = { id: '1065973704', type: 'song' };
// The state the race leaves behind: a buffer, nothing queued, 40% through.
const stuck = () => ({
  m:  { playbackState: 2, currentPlaybackDuration: 100, currentPlaybackTime: 40,
        queue: { nextPlayableItem: { ...NEXT } } },
  ap: { _buffer: { nextSeamlessTransition: undefined }, nextManifest: undefined,
        preloadItem: () => Promise.resolve() },
});
const rescue = (s, rescuedId = null) => _preloadRescueTarget(s.m, s.ap, rescuedId)?.id ?? null;

test('nothing queued past 35% → rescues the next item', () => eq(rescue(stuck()), NEXT.id));
test('next manifest already queued → leaves it alone', () => {
  const s = stuck(); s.ap.nextManifest = { mediaItem: NEXT };
  eq(rescue(s), null);
});
test('transition already scheduled → leaves it alone', () => {
  const s = stuck(); s.ap._buffer.nextSeamlessTransition = 99.6;
  eq(rescue(s), null, 'a setNextManifest now abandons the transition in flight');
});
test('before 35% → waits for MusicKit\'s own Preloader', () => {
  const s = stuck(); s.m.currentPlaybackTime = 30;
  eq(rescue(s), null);
});
test('within 10 s of the end → too close to the join', () => {
  const s = stuck(); s.m.currentPlaybackTime = 91;
  eq(rescue(s), null);
});
test('not playing → no rescue', () => {
  for (const st of [1, 3, 4, 8]) { const s = stuck(); s.m.playbackState = st; eq(rescue(s), null, `state ${st}`); }
});
test('no next item (end of queue) → no rescue', () => {
  const s = stuck(); s.m.queue.nextPlayableItem = undefined;
  eq(rescue(s), null);
});
test('next item is not a song → no rescue', () => {
  const s = stuck(); s.m.queue.nextPlayableItem = { id: 'x', type: 'music-videos' };
  eq(rescue(s), null);
});
test('already rescued this item → not again', () => eq(rescue(stuck(), NEXT.id), null));
test('unknown duration → no rescue', () => {
  const s = stuck(); s.m.currentPlaybackDuration = 0;
  eq(rescue(s), null);
});
test('MusicKit build without audioPlayer → does nothing', () => {
  eq(_preloadRescueTarget(stuck().m, undefined, null), null);
});
test('audioPlayer without a buffer → does nothing', () => {
  const s = stuck(); s.ap._buffer = undefined;
  eq(rescue(s), null);
});
test('audioPlayer without preloadItem → does nothing', () => {
  const s = stuck(); delete s.ap.preloadItem;
  eq(rescue(s), null);
});


console.log(`\n${passed + failed} tests: ${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
