#!/usr/bin/env node
// Behaviour tests for engine.js against fake media elements and Web Audio
// nodes; no browser and no npm dependencies. `go test ./internal/player/local`
// runs this file through Node, and it also runs directly:
//   node internal/player/local/web/engine_test.mjs
// Exit code 0 = all passed, 1 = failures.
import { readFileSync } from 'node:fs';
import vm from 'node:vm';

const source = readFileSync(new URL('./engine.js', import.meta.url), 'utf8');
const sandbox = {};
vm.runInNewContext(source, sandbox, { filename: 'engine.js' });
const createEngine = sandbox.vibezLocalEngine;

// ─── Harness ─────────────────────────────────────────────────────────────────
let passed = 0, failed = 0;
const test = async (name, fn) => {
  try { await fn(); console.log(`  ✓ ${name}`); passed++; }
  catch (e) { console.error(`  ✗ ${name}: ${e.stack || e.message}`); failed++; }
};
const eq = (a, b, msg) => {
  if (JSON.stringify(a) !== JSON.stringify(b))
    throw new Error(`${msg ?? ''} expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}`);
};
const ok = (cond, msg) => { if (!cond) throw new Error(msg); };
const settled = () => new Promise((resolve) => setImmediate(resolve));

// ─── Fakes ───────────────────────────────────────────────────────────────────
class FakeNode {
  constructor(kind) { this.kind = kind; this.outputs = []; }
  connect(node) { this.outputs.push(node); return node; }
  disconnect() { this.outputs = []; }
}

function fakeContext() {
  const created = [];
  const make = (kind, extra) => { const n = Object.assign(new FakeNode(kind), extra); created.push(n); return n; };
  return {
    created,
    state: 'suspended',
    destination: new FakeNode('destination'),
    createGain: () => make('gain', { gain: { value: 1 } }),
    createBiquadFilter: () => make('biquad', {
      type: 'lowpass', frequency: { value: 350 }, Q: { value: 1 }, gain: { value: 0 },
    }),
    createMediaElementSource: (el) => make('source', { el }),
    async resume() { this.state = 'running'; },
  };
}

// FakeAudio models what the engine relies on. behavior picks how play()
// goes: 'start' plays, 'unsupported' fails the way Chrome fails a file it
// cannot decode, 'hang' never settles.
class FakeAudio {
  constructor(behavior) {
    this.behavior = behavior;
    this.listeners = {};
    this.paused = true;
    this.ended = false;
    this.seeking = false;
    this.readyState = 0;
    this.currentTime = 0;
    this.duration = NaN;
    this.error = null;
    this.src = '';
    this.loads = 0;
  }
  addEventListener(type, fn) { (this.listeners[type] ||= []).push(fn); }
  removeEventListener(type, fn) { this.listeners[type] = (this.listeners[type] || []).filter((f) => f !== fn); }
  fire(type) { for (const fn of [...(this.listeners[type] || [])]) fn({ type }); }
  removeAttribute(name) { if (name === 'src') this.src = ''; }
  load() { this.loads++; }
  becomeReady(duration) {
    this.readyState = 4;
    this.duration = duration;
    this.fire('loadedmetadata');
    this.fire('durationchange');
  }
  pause() {
    if (this.paused) return;
    this.paused = true;
    this.fire('pause');
  }
  play() {
    if (this.behavior === 'unsupported') {
      this.error = { code: 4, message: 'DEMUXER_ERROR_NO_SUPPORTED_STREAMS' };
      this.fire('error');
      return Promise.reject(Object.assign(new Error('The element has no supported sources.'), { name: 'NotSupportedError' }));
    }
    this.paused = false;
    if (this.behavior === 'hang') return new Promise(() => {});
    if (this.readyState < 4) this.becomeReady(180);
    this.fire('playing');
    return Promise.resolve();
  }
}

function setup(behavior = 'start') {
  const ctx = fakeContext();
  const events = [];
  const elements = [];
  const timers = new Map();
  let nextTimer = 1;
  const env = {
    behavior,
    clock: 0,
    audioContext: ctx,
    createAudio: () => { const el = new FakeAudio(env.behavior); elements.push(el); return el; },
    emit: (ev) => events.push(ev),
    now: () => env.clock,
    setTimeout: (fn) => { const id = nextTimer++; timers.set(id, fn); return id; },
    clearTimeout: (id) => { timers.delete(id); },
  };
  const engine = createEngine(env);
  const output = ctx.created.find((n) => n.kind === 'gain');
  const call = async (name, ...args) => JSON.parse(await engine.call(name, args));
  const sourceOf = (el) => ctx.created.find((n) => n.kind === 'source' && n.el === el);
  const fireTimers = () => { const fns = [...timers.values()]; timers.clear(); fns.forEach((fn) => fn()); };
  return { ctx, env, engine, events, elements, output, call, sourceOf, fireTimers };
}

// chain follows the single path from node to wherever it ends.
function chain(node) {
  const path = [];
  for (let n = node; n; n = n.outputs[0]) {
    if (n.outputs.length > 1) throw new Error(`${n.kind} feeds ${n.outputs.length} nodes`);
    path.push(n.kind === 'biquad' ? n.type : n.kind);
  }
  return path;
}

const BANDS = [
  { frequency: 32, q: 1.46, gain: 3 },
  { frequency: 1000, q: 1.46, gain: -2.5 },
  { frequency: 16000, q: 1.46, gain: 6 },
];

// ─── Loading and playback ────────────────────────────────────────────────────
console.log('\nloading and playback');

await test('autoplay load starts playback and reports its generation', async () => {
  const h = setup();
  const r = await h.call('load', 'http://127.0.0.1:1/t/media/a', 7, true);
  eq([r.gen, r.playing, r.loading, r.duration, r.error], [7, true, false, 180, '']);
  eq(h.elements[0].src, 'http://127.0.0.1:1/t/media/a');
  eq(h.ctx.state, 'running', 'a suspended AudioContext must be resumed before play');
});

await test('load without autoplay waits for metadata and stays paused', async () => {
  const h = setup();
  const pending = h.call('load', 'u', 1, false);
  await settled();
  h.elements[0].becomeReady(95.5);
  const r = await pending;
  eq([r.gen, r.playing, r.duration, r.error], [1, false, 95.5, '']);
});

await test('an undecodable file fails the load with the media reason, reported once', async () => {
  const h = setup('unsupported');
  const r = await h.call('load', 'u', 1, true);
  ok(r.error.startsWith('unsupported format'), `error = ${r.error}`);
  eq(r.playing, false);
  eq(h.events.filter((e) => e.kind === 'error').length, 0, 'error events:');
});

await test('a media error after playback started is reported as an event', async () => {
  const h = setup();
  await h.call('load', 'u', 3, true);
  h.elements[0].error = { code: 3, message: 'PIPELINE_ERROR_DECODE' };
  h.elements[0].fire('error');
  const ev = h.events.at(-1);
  eq([ev.kind, ev.gen], ['error', 3]);
  ok(ev.error.startsWith('decode error'), `error = ${ev.error}`);
});

await test('a start that never settles times out and is paused', async () => {
  const h = setup('hang');
  const pending = h.call('load', 'u', 1, true);
  await settled();
  h.fireTimers();
  const r = await pending;
  ok(r.error.includes('timed out'), `error = ${r.error}`);
  eq(h.elements[0].paused, true, 'element paused:');
});

await test('pause, play, seek and stop act on the loaded track', async () => {
  const h = setup();
  await h.call('load', 'u', 1, true);
  eq((await h.call('pause')).playing, false);
  eq((await h.call('play')).playing, true);
  eq((await h.call('seek', 42.5)).position, 42.5);
  const r = await h.call('stop');
  eq([r.playing, r.position], [false, 0]);
});

await test('commands with no track loaded fail without throwing', async () => {
  const h = setup();
  for (const name of ['play', 'pause', 'stop', 'seek']) {
    eq((await h.call(name, 1)).error, 'no track is loaded', `${name}:`);
  }
});

await test('unknown commands are rejected', async () => {
  const h = setup();
  let threw = false;
  try { await h.engine.call('constructor', []); } catch { threw = true; }
  ok(threw, 'call("constructor") resolved');
});

await test('volume is applied on the output gain', async () => {
  const h = setup();
  await h.call('setVolume', 0.25);
  eq(h.output.gain.value, 0.25);
});

// ─── Generations and events ─────────────────────────────────────────────────
console.log('\ngenerations and events');

await test('events of a replaced track are not reported', async () => {
  const h = setup();
  await h.call('load', 'a', 1, true);
  await h.call('load', 'b', 2, true);
  h.events.length = 0;
  h.elements[0].fire('ended');
  h.elements[0].fire('error');
  eq(h.events, [], 'events from the replaced element:');
  h.elements[1].fire('ended');
  eq(h.events.map((e) => [e.kind, e.gen]), [['ended', 2]]);
});

await test('a replaced track is released', async () => {
  const h = setup();
  await h.call('load', 'a', 1, true);
  await h.call('load', 'b', 2, true);
  const old = h.elements[0];
  eq([old.paused, old.src, old.loads], [true, '', 1]);
  eq(h.sourceOf(old).outputs, [], 'old source outputs:');
});

await test('unload drops the track and its events', async () => {
  const h = setup();
  await h.call('load', 'a', 1, true);
  const r = await h.call('unload');
  eq([r.gen, r.playing], [0, false]);
  h.events.length = 0;
  h.elements[0].fire('ended');
  eq(h.events, []);
});

await test('position updates are throttled while playing', async () => {
  const h = setup();
  await h.call('load', 'a', 1, true);
  h.events.length = 0;
  for (const t of [100, 200, 599, 600, 700]) { h.env.clock = t; h.elements[0].fire('timeupdate'); }
  eq(h.events.filter((e) => e.kind === 'timeupdate').length, 2);
});

await test('snapshots are sequenced across replies and events', async () => {
  const h = setup();
  const seqs = [];
  seqs.push((await h.call('load', 'a', 1, true)).seq);
  h.elements[0].fire('pause');
  seqs.push(h.events.at(-1).seq);
  seqs.push((await h.call('seek', 3)).seq);
  h.elements[0].fire('seeked');
  seqs.push(h.events.at(-1).seq);
  ok(seqs.every((s, i) => i === 0 || s > seqs[i - 1]), `seqs = ${seqs}`);
});

// ─── Equalizer ───────────────────────────────────────────────────────────────
console.log('\nequalizer');

await test('a track plays straight into the output with no EQ', async () => {
  const h = setup();
  await h.call('load', 'a', 1, true);
  eq(chain(h.sourceOf(h.elements[0])), ['source', 'gain', 'destination']);
});

await test('EQ bands become shelves at the ends and peaks between', async () => {
  const h = setup();
  await h.call('load', 'a', 1, true);
  await h.call('setEqualizer', BANDS);
  eq(chain(h.sourceOf(h.elements[0])), ['source', 'lowshelf', 'peaking', 'highshelf', 'gain', 'destination']);
  const filters = h.ctx.created.filter((n) => n.kind === 'biquad');
  eq(filters.map((f) => [f.frequency.value, f.Q.value, f.gain.value]), BANDS.map((b) => [b.frequency, b.q, b.gain]));
});

await test('the EQ carries over to the next track', async () => {
  const h = setup();
  await h.call('setEqualizer', BANDS);
  await h.call('load', 'a', 1, true);
  await h.call('load', 'b', 2, true);
  eq(chain(h.sourceOf(h.elements[1])), ['source', 'lowshelf', 'peaking', 'highshelf', 'gain', 'destination']);
});

await test('clearing the EQ bypasses and disconnects the old filters', async () => {
  const h = setup();
  await h.call('load', 'a', 1, true);
  await h.call('setEqualizer', BANDS);
  await h.call('setEqualizer', []);
  eq(chain(h.sourceOf(h.elements[0])), ['source', 'gain', 'destination']);
  ok(h.ctx.created.filter((n) => n.kind === 'biquad').every((f) => f.outputs.length === 0), 'old filters still connected');
});

console.log(`\n${passed} passed, ${failed} failed`);
process.exit(failed ? 1 : 0);
