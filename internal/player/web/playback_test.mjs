import { readFileSync } from 'node:fs';
import { runInNewContext } from 'node:vm';
import test from 'node:test';
import assert from 'node:assert/strict';

const html = readFileSync(new URL('./musickit.html', import.meta.url), 'utf8');
function functionSource(name) {
 const start = html.indexOf(`async function ${name}(`);
 if (start < 0) return '';
 const end = html.indexOf('\n        }', start) + '\n        }'.length;
 return html.slice(start, end);
}
function setup(music) {
 const errors = [];
 const env = {
  window: {}, _m: () => music, _log: () => {},
  _stateN: [], _qi: 0, _q: [{id:'123'}], _busy: false,
  _wantIdx: -1, _wantNative: false, _nativeQueue: false, _nativeMirrors: false,
  _playPending: null, _removedIds: new Set(),
  _playAt: () => {}, _stopAndWait: async () => {}, _warmLibItems: () => {},
  _runPending: () => {}, errName: e => e.message,
  goError: text => { errors.push(text); return Promise.resolve(); },
 };
 const start = html.indexOf('window.vibezPlay = async () =>');
 const end = html.indexOf('window.vibezPause = async () =>', start);
 runInNewContext(functionSource('_ensurePlaying') + '\n' + html.slice(start, end) + '\n' + functionSource('_doPlayNativeAt'), env);
 return {env, errors};
}
function music(state) {
 return {
  playbackState: state, nowPlayingItem: {id:'123'}, plays:0, pauses:0, stops:0,
  async play() {
   this.plays++;
   if (![3,4].includes(this.playbackState)) throw new Error('The play() method was called without a previous stop() or pause() call.');
   await new Promise(resolve => setTimeout(resolve, 1));
   this.playbackState = 2;
  },
  async pause() { this.pauses++; this.playbackState=3; },
  async stop() { this.stops++; this.playbackState=4; },
  async setQueue() { this.playbackState=2; },
 };
}
test('play on an already playing track is a no-op', async () => {
 const m=music(2); const {env,errors}=setup(m);
 await env.window.vibezPlay();
 assert.deepEqual(errors, []); assert.equal(m.plays,0);
});
test('concurrent resume requests call MusicKit play once', async () => {
 const m=music(3); const {env,errors}=setup(m);
 await Promise.all([env.window.vibezPlay(),env.window.vibezPlay()]);
 assert.deepEqual(errors, []); assert.equal(m.plays,1); assert.equal(m.stops,0);
});
test('resume from loading pauses before play without resetting the track', async () => {
 const m=music(1); const {env,errors}=setup(m);
 await env.window.vibezPlay();
 assert.deepEqual(errors, []); assert.equal(m.pauses,1); assert.equal(m.stops,0);
 assert.equal(m.playbackState,2);
});
test('native setQueue auto-play does not trigger a duplicate play and fallback', async () => {
 const m=music(4); const {env}=setup(m);
 await env._doPlayNativeAt(0);
 assert.equal(env._nativeQueue,true); assert.equal(m.plays,0);
});

// ─── vibezQueuePlayNext (#161) ───────────────────────────────────────────────
const libSongs = {
 'i.L1': { id: 'i.L1', attributes: { playParams: { id: 'i.L1', kind: 'song', catalogId: '901' } } },
 'i.U1': { id: 'i.U1', attributes: { playParams: { id: 'i.U1', kind: 'song' } } }, // upload, no catalog copy
};
const catalogSong = id => ({ id, attributes: { playParams: { id, kind: 'song' } } });
function setupPlayNext({ native, mirrors = true, qi = 1, playNextFails = false, catalog = {}, lookupFails = false, onLookup = () => {} } = {}) {
 const calls = [];
 const m = {
  storefrontId: 'us',
  api: { async music(path, { ids }) {
   calls.push(['lookup', ids.join(',')]);
   onLookup(env);
   if (lookupFails) throw new Error('offline');
   return { data: { data: ids.filter(id => catalog[id]).map(id => catalog[id]) } };
  } },
  async playNext(d) { calls.push(['playNext', d.items.map(i => i.id).join(',')]); if (playNextFails) throw new Error('nope'); },
  async playLater(d) { calls.push(['playLater', d.items.map(i => i.id).join(',')]); },
 };
 const env = {
  window: {}, _m: () => m, _log: () => {},
  _q: [{id:'a'}, {id:'b'}, {id:'c'}], _qi: qi,
  _nativeQueue: native, _nativeMirrors: mirrors, _removedIds: new Set(['x']),
  _libMap: {}, _libItems: {},
  _resolveItems: async ids => ids.map(id => libSongs[id] ?? {id}),
  _playAt: idx => calls.push(['playAt', idx]), _warmLibItems: () => {},
  errName: e => e.message, goError: () => Promise.resolve(),
 };
 const start = html.indexOf('window.vibezQueuePlayNext = async function');
 const end = html.indexOf('window.vibezQueueRemove = function', start);
 runInNewContext(html.slice(start, end), env);
 return { env, calls, ids: () => env._q.map(i => i.id).join(',') };
}
test('playNext in native mode inserts after the current item in both queues', async () => {
 const { env, calls, ids } = setupPlayNext({ native: true });
 await env.window.vibezQueuePlayNext('["x","y"]');
 assert.equal(ids(), 'a,b,x,y,c');
 assert.deepEqual(calls.map(c => c.join(':')), ['playNext:x,y']);
 assert.equal(env._nativeMirrors, true, 'MusicKit inserted at the same place, so _q still mirrors it');
 assert.equal(env._removedIds.has('x'), false, 're-added item must not be skipped on arrival');
});
test('playNext in one-item mode only touches _q', async () => {
 const { env, calls, ids } = setupPlayNext({ native: false });
 await env.window.vibezQueuePlayNext('["x"]');
 assert.equal(ids(), 'a,b,x,c');
 assert.equal(calls.length, 0);
});
test('playNext falls back to playLater and drops mirroring when playNext throws', async () => {
 const { env, calls } = setupPlayNext({ native: true, playNextFails: true });
 await env.window.vibezQueuePlayNext('["x"]');
 assert.deepEqual(calls.map(c => c.join(':')), ['playNext:x', 'playLater:x']);
 assert.equal(env._nativeMirrors, false);
});
test('playNext with nothing playing appends and starts', async () => {
 const { env, calls, ids } = setupPlayNext({ native: false, qi: -1 });
 await env.window.vibezQueuePlayNext('["x"]');
 assert.equal(ids(), 'a,b,c,x');
 assert.deepEqual(calls.map(c => c.join(':')), ['playAt:0']);
});
test('playNext in native mode swaps a library item for its playable catalog version', async () => {
 const { env, calls, ids } = setupPlayNext({ native: true, catalog: { '901': catalogSong('901') } });
 await env.window.vibezQueuePlayNext('["i.L1","i.U1","x"]');
 assert.equal(ids(), 'a,b,901,i.U1,x,c', '_q must hold the same objects MusicKit does');
 assert.deepEqual(calls.map(c => c.join(':')), ['lookup:901', 'playNext:901,i.U1,x']);
 assert.equal(env._libMap['901'], 'i.L1', 'CONTENT_UNAVAILABLE fallback must still find the library copy');
 assert.equal(env._libItems['i.L1'].id, 'i.L1');
});
test('playNext keeps the library item when the catalog version is not playable', async () => {
 const { env, calls, ids } = setupPlayNext({ native: true, catalog: {} });
 await env.window.vibezQueuePlayNext('["i.L1"]');
 assert.equal(ids(), 'a,b,i.L1,c');
 assert.deepEqual(calls.map(c => c.join(':')), ['lookup:901', 'playNext:i.L1']);
});
test('playNext keeps library items when the catalog lookup fails', async () => {
 const { env, calls, ids } = setupPlayNext({ native: true, lookupFails: true });
 await env.window.vibezQueuePlayNext('["i.L1"]');
 assert.equal(ids(), 'a,b,i.L1,c');
 assert.deepEqual(calls.map(c => c.join(':')), ['lookup:901', 'playNext:i.L1']);
});
test('playNext appends and starts when playback stops during the catalog lookup', async () => {
 const { env, calls, ids } = setupPlayNext({ native: true, catalog: { '901': catalogSong('901') },
  onLookup: env => { env._q = []; env._qi = -1; } });
 await env.window.vibezQueuePlayNext('["i.L1"]');
 assert.equal(ids(), 'i.L1');
 assert.deepEqual(calls.map(c => c.join(':')), ['lookup:901', 'playAt:0'], 'no playNext on a stopped queue');
});
test('playNext in one-item mode does not swap library items', async () => {
 const { env, calls, ids } = setupPlayNext({ native: false, catalog: { '901': catalogSong('901') } });
 await env.window.vibezQueuePlayNext('["i.L1"]');
 assert.equal(ids(), 'a,b,i.L1,c');
 assert.equal(calls.length, 0);
});

// ─── Following a reordered _q (#161) ─────────────────────────────────────────
// A fake native queue with the behaviour the #161 probe measured: playNext
// inserts at position + 1, so the latest call is always next, and nothing
// can be removed.
function nativeQueue(ids, position = 0) {
 const calls = [];
 const m = {
  repeatMode: 0, shuffleMode: 0, playbackState: 2,
  currentPlaybackTime: 0, currentPlaybackDuration: 200,
  queue: { items: ids.map(id => ({ id })), position },
  get nowPlayingItem() { return this.queue.items[this.queue.position]; },
  async playNext(d) {
   calls.push('playNext:' + d.items.map(i => i.id).join(','));
   this.queue.items.splice(this.queue.position + 1, 0, ...d.items.map(i => ({ id: i.id })));
  },
  async skipToNextItem() { calls.push('skipToNext'); this.queue.position++; },
  async skipToPreviousItem() { calls.push('skipToPrevious'); this.queue.position--; },
  async stop() { calls.push('stop'); },
 };
 m.queue.remove = async i => { calls.push('remove:' + m.queue.items[i].id); m.queue.items.splice(i, 1); };
 return { m, calls };
}
const between = (from, to) => {
 const start = html.indexOf(from);
 const end = html.indexOf(to, start);
 assert.ok(start >= 0 && end > start, `musickit.html no longer has ${from}`);
 return html.slice(start, end);
};
function setupFollow({ q, qi = 0, native, position = qi, mirrors = true }) {
 const { m, calls } = nativeQueue(native ?? q, position);
 const env = {
  window: {}, music: { addEventListener: () => {} }, _m: () => m, _log: () => {},
  _q: q.map(id => ({ id })), _qi: qi, _busy: false,
  _nativeQueue: true, _nativeMirrors: mirrors, _removedIds: new Set(),
  _nativeHasNext: mm => mm.repeatMode !== 0 || mm.queue.position < mm.queue.items.length - 1,
  _playNativeAt: idx => calls.push('rebuild:' + idx),
  _playAt: idx => calls.push('playAt:' + idx),
  errName: e => e.message, goError: () => Promise.resolve(),
  setTimeout: (...a) => setTimeout(...a), clearTimeout: t => clearTimeout(t),
 };
 runInNewContext([
  between('        // Following _q: MusicKit plays', "music.addEventListener('nowPlayingItemDidChange', _syncQueueIndex);"),
  between('        let _stallTimer = null;', "music.addEventListener('playbackStateDidChange'"),
  between('window.vibezNext  = () =>', 'const _repeatName'),
  between('window.vibezQueueRemove = function', 'window.vibezQueueClear'),
 ].join('\n'), env);
 const ids = () => env._q.map(i => i.id).join(',');
 // Let MusicKit advance on its own, as at a natural track boundary.
 const advance = async () => {
  m.queue.position++;
  env._syncQueueIndex();
  await env._syncNext();
 };
 return { env, m, calls, ids, advance };
}

test('a move makes MusicKit play the moved track next without touching the current one', async () => {
 const { env, m, calls, ids } = setupFollow({ q: ['a', 'b', 'c', 'd'] });
 env.window.vibezQueueMove(3, 1);
 await env._syncNext();
 assert.equal(ids(), 'a,d,b,c');
 assert.deepEqual(calls, ['playNext:d']);
 assert.equal(m.queue.position, 0, 'the current track keeps playing');
 assert.equal(m.queue.items[1].id, 'd');
});
test('playback follows the reordered _q to the end, and the stale tail is dropped', async () => {
 const { env, m, calls, advance } = setupFollow({ q: ['a', 'b', 'c', 'd'] });
 env.window.vibezQueueMove(3, 1);   // a,d,b,c
 env.window.vibezQueueMove(3, 2);   // a,d,c,b
 await env._syncNext();
 const played = [m.nowPlayingItem.id];
 for (let i = 0; i < 3; i++) { await advance(); played.push(m.nowPlayingItem.id); }
 assert.deepEqual(played, ['a', 'd', 'c', 'b']);
 assert.equal(env._qi, 3);
 // Stale items must never start: Apple logs a play the moment one does.
 assert.equal(m.queue.items.length, m.queue.position + 1, 'nothing left after the last item');
 assert.deepEqual(calls.filter(c => c.startsWith('remove:')), ['remove:d', 'remove:c']);
});
test('without queue.remove, a stale item past the end is stopped when it starts', async () => {
 const { env, m, calls, advance } = setupFollow({ q: ['a', 'b'], qi: 1, native: ['a', 'b', 'x'], mirrors: false });
 delete m.queue.remove;
 await env._syncNext();
 await advance();
 assert.equal(calls.at(-1), 'stop');
 assert.equal(env._nativeQueue, false);
});
test('a failing queue.remove leaves the stop-on-arrival fallback in place', async () => {
 const { env, m, calls, advance } = setupFollow({ q: ['a', 'b'], qi: 1, native: ['a', 'b', 'x'], mirrors: false });
 m.queue.remove = async () => { throw new Error('deprecated'); };
 await env._syncNext();
 await advance();
 assert.equal(calls.at(-1), 'stop');
});
test('nothing is dropped while a next item is still wanted', async () => {
 const { env, calls } = setupFollow({ q: ['a', 'b', 'c'], native: ['a', 'b', 'c', 'x'], mirrors: false });
 await env._syncNext();
 assert.deepEqual(calls, []);
});
test('a next that already matches _q is left alone', async () => {
 const { env, calls } = setupFollow({ q: ['a', 'b', 'c'], native: ['a', 'b', 'x'], mirrors: false });
 await env._syncNext();
 assert.deepEqual(calls, []);
});
test('nothing is synced while _q still mirrors MusicKit or under native shuffle', async () => {
 const mirrored = setupFollow({ q: ['a', 'b'], native: ['a', 'x'] });
 await mirrored.env._syncNext();
 assert.deepEqual(mirrored.calls, []);
 const shuffled = setupFollow({ q: ['a', 'b'], native: ['a', 'x'], mirrors: false });
 shuffled.m.shuffleMode = 1;
 await shuffled.env._syncNext();
 assert.deepEqual(shuffled.calls, []);
});
test('repeat-all at the last item makes the first item next', async () => {
 const { env, m, calls } = setupFollow({ q: ['a', 'b', 'c'], qi: 2, native: ['a', 'b', 'c'], mirrors: false });
 m.repeatMode = 2;
 await env._syncNext();
 assert.deepEqual(calls, ['playNext:a']);
 m.queue.position++;
 env._syncQueueIndex();
 assert.equal(env._qi, 0);
});
test('removing the next item replaces it in MusicKit', async () => {
 const { env, calls, ids } = setupFollow({ q: ['a', 'b', 'c'] });
 env.window.vibezQueueRemove('1');
 await env._syncNext();
 assert.equal(ids(), 'a,c');
 assert.deepEqual(calls, ['playNext:c']);
});
test('an unexpected arrival is rebuilt from _q at the index that should be playing', async () => {
 const { env, m, calls } = setupFollow({ q: ['a', 'b', 'c'], native: ['a', 'x', 'b', 'c'], mirrors: false });
 m.queue.position = 1; // the correction did not land before the boundary
 env._syncQueueIndex();
 assert.deepEqual(calls, ['rebuild:1']);
});
test('arrivals during a rebuild are left to the rebuild', async () => {
 const { env, m, calls } = setupFollow({ q: ['a', 'b', 'c'], native: ['a', 'x'], mirrors: false });
 env._busy = true;
 m.queue.position = 1;
 env._syncQueueIndex();
 assert.deepEqual(calls, []);
 assert.equal(env._qi, 0);
});
test('next at the end of _q does not skip into a stale item', () => {
 const { env, calls } = setupFollow({ q: ['a', 'b'], qi: 1, native: ['a', 'b', 'x'], mirrors: false });
 env.window.vibezNext();
 assert.deepEqual(calls, []);
});
test('prev steps back natively when MusicKit history matches _q, and rebuilds otherwise', () => {
 const agree = setupFollow({ q: ['a', 'b', 'c'], qi: 1, native: ['a', 'b', 'c'], mirrors: false });
 agree.env.window.vibezPrev();
 assert.deepEqual(agree.calls, ['skipToPrevious']);
 agree.env._syncQueueIndex();
 assert.equal(agree.env._qi, 0);
 // a was moved after b while b played: MusicKit's history still says a.
 const moved = setupFollow({ q: ['c', 'b', 'a'], qi: 1, native: ['a', 'b', 'c'], mirrors: false });
 moved.env.window.vibezPrev();
 assert.deepEqual(moved.calls, ['rebuild:0']);
});
test('a stall at the end of a track rebuilds at the next index', async (t) => {
 t.mock.timers.enable({ apis: ['setTimeout'] });
 const { env, m, calls } = setupFollow({ q: ['a', 'b', 'c'], mirrors: false });
 m.playbackState = 8; m.currentPlaybackTime = 199;
 env._watchStall(m);
 t.mock.timers.tick(3000);
 assert.deepEqual(calls, ['rebuild:1']);
});
test('waiting mid-track, or a stall that clears, is left alone', async (t) => {
 t.mock.timers.enable({ apis: ['setTimeout'] });
 const mid = setupFollow({ q: ['a', 'b'], mirrors: false });
 mid.m.playbackState = 8; mid.m.currentPlaybackTime = 60;
 mid.env._watchStall(mid.m);
 t.mock.timers.tick(3000);
 assert.deepEqual(mid.calls, []);
 const cleared = setupFollow({ q: ['a', 'b'], mirrors: false });
 cleared.m.playbackState = 8; cleared.m.currentPlaybackTime = 199;
 cleared.env._watchStall(cleared.m);
 cleared.m.playbackState = 2;
 cleared.env._watchStall(cleared.m);
 t.mock.timers.tick(3000);
 assert.deepEqual(cleared.calls, []);
});
