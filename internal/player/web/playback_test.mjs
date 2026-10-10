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
function setupPlayNext({ native, mirrors = true, qi = 1, playNextFails = false } = {}) {
 const calls = [];
 const m = {
  async playNext(d) { calls.push(['playNext', d.items.map(i => i.id).join(',')]); if (playNextFails) throw new Error('nope'); },
  async playLater(d) { calls.push(['playLater', d.items.map(i => i.id).join(',')]); },
 };
 const env = {
  window: {}, _m: () => m, _log: () => {},
  _q: [{id:'a'}, {id:'b'}, {id:'c'}], _qi: qi,
  _nativeQueue: native, _nativeMirrors: mirrors, _removedIds: new Set(['x']),
  _resolveItems: async ids => ids.map(id => ({id})),
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
