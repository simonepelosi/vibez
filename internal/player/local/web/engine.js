// Audio engine for vibez's Chrome-backed local file playback (Windows).
//
// Go owns the queue and decides what plays next. This engine plays the one
// track it was told to load and reports what the audio element is actually
// doing. Each track gets a fresh <audio> element tagged with the generation Go
// assigned to it, so events still queued for a replaced element are dropped
// here and, should one slip through, recognisably stale in Go. Every snapshot
// carries a sequence number because Playwright delivers bindings
// concurrently, and Go must not let an older snapshot overwrite a newer one.
//
// Signal path: element → MediaElementSource → EQ filters → gain → output.
// Volume is set on the gain node, not on HTMLMediaElement.volume, so it applies
// the same way whatever the browser does with a captured element's volume.
(function (root) {
  'use strict';

  const TICK_MS = 500;             // spacing of position updates while playing
  const START_TIMEOUT_MS = 15000;  // bound on waiting for a track to start
  const HAVE_METADATA = 1;
  const HAVE_FUTURE_DATA = 3;
  const MEDIA_ERRORS = ['', 'playback aborted', 'network error', 'decode error', 'unsupported format'];
  const FORWARDED = ['playing', 'pause', 'waiting', 'seeked', 'durationchange', 'ended', 'error', 'timeupdate'];

  // env supplies the browser pieces, so tests can run this without one:
  //   audioContext  an AudioContext
  //   createAudio() a new HTMLAudioElement
  //   emit(event)   delivers an event snapshot to Go
  //   now()         milliseconds, monotonic
  //   setTimeout / clearTimeout
  function createEngine(env) {
    const ctx = env.audioContext;
    const output = ctx.createGain();
    output.connect(ctx.destination);
    let filters = [];
    let track = null; // { gen, el, source, pending, lastTick }
    let seq = 0;

    function snapshot(kind, error) {
      seq += 1;
      const snap = { gen: 0, seq, kind, playing: false, loading: false, position: 0, duration: 0, error: error || '' };
      if (!track) return snap;
      const el = track.el;
      const running = !el.paused && !el.ended;
      snap.gen = track.gen;
      snap.playing = running && !el.seeking && el.readyState >= HAVE_FUTURE_DATA;
      snap.loading = running && !snap.playing;
      snap.position = Number.isFinite(el.currentTime) ? el.currentTime : 0;
      snap.duration = Number.isFinite(el.duration) ? el.duration : 0;
      return snap;
    }

    function describe(el, err) {
      const me = el.error;
      if (me) {
        const what = MEDIA_ERRORS[me.code] || `media error ${me.code}`;
        return me.message ? `${what} (${me.message})` : what;
      }
      if (err && err.name && err.message) return `${err.name}: ${err.message}`;
      return String((err && err.message) || err || 'unknown error');
    }

    function withTimeout(promise, what) {
      let timer;
      const timeout = new Promise((_, reject) => {
        timer = env.setTimeout(() => reject(new Error(`${what} timed out`)), START_TIMEOUT_MS);
      });
      return Promise.race([promise, timeout]).finally(() => env.clearTimeout(timer));
    }

    function metadata(el) {
      return new Promise((resolve, reject) => {
        if (el.readyState >= HAVE_METADATA) { resolve(); return; }
        const finish = (fn) => () => {
          el.removeEventListener('loadedmetadata', ok);
          el.removeEventListener('error', fail);
          fn();
        };
        const ok = finish(resolve);
        const fail = finish(() => reject(el.error));
        el.addEventListener('loadedmetadata', ok);
        el.addEventListener('error', fail);
      });
    }

    // forward reports an element event to Go. An error while a command is
    // waiting on the element is left to that command's reply, so it reaches
    // Go once, as the command's failure.
    function forward(t, type) {
      if (track !== t) return;
      if (type === 'error') {
        if (!t.pending) env.emit(snapshot('error', describe(t.el)));
        return;
      }
      if (type === 'timeupdate') {
        const now = env.now();
        if (now - t.lastTick < TICK_MS) return;
        t.lastTick = now;
      }
      env.emit(snapshot(type));
    }

    // settle runs a command that waits on the element and turns its outcome
    // into the reply. A failed start is paused so the element cannot begin
    // playing later, after Go has been told it did not.
    async function settle(t, work, what) {
      t.pending = true;
      try {
        await withTimeout(work(), what);
        return snapshot('');
      } catch (err) {
        t.el.pause();
        return snapshot('', describe(t.el, err));
      } finally {
        t.pending = false;
      }
    }

    function start(t) {
      return settle(t, async () => {
        if (ctx.state === 'suspended') await ctx.resume();
        await t.el.play();
      }, 'playback start');
    }

    function release() {
      const t = track;
      if (!t) return;
      track = null;
      t.el.pause();
      t.source.disconnect();
      // Dropping the source and reloading aborts the fetch and frees the
      // decoder instead of leaving both to garbage collection.
      t.el.removeAttribute('src');
      t.el.load();
    }

    function load(url, gen, autoplay) {
      release();
      const el = env.createAudio();
      el.preload = 'auto';
      const t = { gen, el, source: ctx.createMediaElementSource(el), pending: false, lastTick: -Infinity };
      track = t;
      t.source.connect(filters[0] || output);
      for (const type of FORWARDED) el.addEventListener(type, () => forward(t, type));
      el.src = url;
      if (autoplay) return start(t);
      return settle(t, () => metadata(el), 'loading');
    }

    function setEqualizer(bands) {
      for (const f of filters) f.disconnect();
      filters = (bands || []).map((b, i, all) => {
        const f = ctx.createBiquadFilter();
        // Shelves at the ends and peaks between, as the MusicKit backend does.
        f.type = i === 0 ? 'lowshelf' : i === all.length - 1 ? 'highshelf' : 'peaking';
        f.frequency.value = b.frequency;
        f.Q.value = b.q;
        f.gain.value = b.gain;
        return f;
      });
      filters.forEach((f, i) => f.connect(filters[i + 1] || output));
      if (track) {
        track.source.disconnect();
        track.source.connect(filters[0] || output);
      }
      return snapshot('');
    }

    const onTrack = (fn) => (...args) => (track ? fn(track, ...args) : snapshot('', 'no track is loaded'));

    const commands = {
      load,
      play: onTrack((t) => start(t)),
      pause: onTrack((t) => { t.el.pause(); return snapshot(''); }),
      stop: onTrack((t) => { t.el.pause(); t.el.currentTime = 0; return snapshot(''); }),
      seek: onTrack((t, seconds) => { t.el.currentTime = seconds; return snapshot(''); }),
      setVolume(v) { output.gain.value = v; return snapshot(''); },
      setEqualizer,
      unload() { release(); return snapshot(''); },
    };

    return {
      // call runs a command and resolves to its reply snapshot as JSON. A
      // command that fails for a media reason replies with error set rather
      // than throwing, so Go gets the reason without Playwright's wrapping.
      async call(name, args) {
        if (!Object.prototype.hasOwnProperty.call(commands, name)) {
          throw new Error(`unknown command: ${name}`);
        }
        return JSON.stringify(await commands[name](...(args || [])));
      },
    };
  }

  root.vibezLocalEngine = createEngine;
})(globalThis);
