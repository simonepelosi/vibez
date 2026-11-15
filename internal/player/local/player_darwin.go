//go:build darwin

// Package local provides a Player backed by CoreAudio for local audio files.
// It uses ExtAudioFile for format-agnostic decoding and AudioQueue for
// PCM output. Both frameworks ship with every macOS installation.
package local

/*
#cgo LDFLAGS: -framework AudioToolbox -framework CoreFoundation
#include <AudioToolbox/AudioToolbox.h>
#include <CoreFoundation/CoreFoundation.h>
#include <limits.h>
#include <math.h>
#include <pthread.h>
#include <stdlib.h>
#include <string.h>

// the buffers in the queue
#define kNumBuffers 3
// Each buffer holds kBufferSeconds of audio, sized from the file's own rate so
// a 192 kHz file gets the same headroom against underruns as a 44.1 kHz one.
#define kBufferSeconds 0.2
#define kEQBands 10

static const double kEQBandHz[kEQBands] = {29, 59, 119, 237, 474, 947, 1889, 3770, 7523, 15011};
#define kEQBandQ 1.4

typedef struct {
	double b0, b1, b2, a1, a2;
	double z1[2], z2[2];
	int active;
} vibez_biquad;

static void vibez_biquad_design(vibez_biquad *bq, double fs, double f0, double gainDb) {
	if (!isfinite(fs) || !isfinite(f0) || !isfinite(gainDb) || fs <= 0.0 || f0 <= 0.0 || fabs(gainDb) < 0.01 || f0 >= fs * 0.45) {
		bq->z1[0] = bq->z1[1] = 0.0;
		bq->z2[0] = bq->z2[1] = 0.0;
		bq->active = 0;
		return;
	}
	double A = pow(10.0, gainDb / 40.0);
	double w0 = 2.0 * 3.14159265358979323846 * f0 / fs;
	double alpha = sin(w0) / (2.0 * kEQBandQ);
	double cw = cos(w0);
	double a0 = 1.0 + alpha / A;
	bq->b0 = (1.0 + alpha * A) / a0;
	bq->b1 = (-2.0 * cw) / a0;
	bq->b2 = (1.0 - alpha * A) / a0;
	bq->a1 = (-2.0 * cw) / a0;
	bq->a2 = (1.0 - alpha / A) / a0;
	bq->active = 1;
}

static double vibez_biquad_run(vibez_biquad *bq, double x, int ch) {
	if (!isfinite(x)) x = 0.0;
	double y = bq->b0 * x + bq->z1[ch];
	bq->z1[ch] = bq->b1 * x - bq->a1 * y + bq->z2[ch];
	bq->z2[ch] = bq->b2 * x - bq->a2 * y;
	if (!isfinite(y) || !isfinite(bq->z1[ch]) || !isfinite(bq->z2[ch])) {
		bq->z1[ch] = 0.0;
		bq->z2[ch] = 0.0;
		return 0.0;
	}
	return y;
}

typedef struct {
	AudioQueueRef queue;
	ExtAudioFileRef file;
	AudioStreamBasicDescription format;
	AudioQueueBufferRef bufs[kNumBuffers];
	UInt32 bufFrames;
	pthread_mutex_t eqMu;
	vibez_biquad eq[kEQBands];
	int done;
	int playing;
	uintptr_t goPlayer;
} vibez_audio_state;

static void vibez_set_eq_gains(vibez_audio_state *s, const double *gains, int count) {
	if (!s) return;
	double fs = (isfinite(s->format.mSampleRate) && s->format.mSampleRate > 0) ? s->format.mSampleRate : 44100.0;
	pthread_mutex_lock(&s->eqMu);
	for (int i = 0; i < kEQBands; i++) {
		double g = (gains && i < count) ? gains[i] : 0.0;
		if (!isfinite(g)) g = 0.0;
		if (g > 24.0) g = 24.0;
		if (g < -24.0) g = -24.0;
		vibez_biquad_design(&s->eq[i], fs, kEQBandHz[i], g);
	}
	pthread_mutex_unlock(&s->eqMu);
}

// This is called from C back into GO when the track ends
extern void vibezOnEOS(uintptr_t handle);

// forward declaration
static void vibez_audio_callback(void* ctx, AudioQueueRef queue, AudioQueueBufferRef buf);

// vibez_fill_buffer reads decoded PCM from ExtAudioFile into an AudioQueue buffer.
// Returns 1 if EOS was reached, 0 otherwise.
static int vibez_fill_buffer(vibez_audio_state *s, AudioQueueBufferRef buf) {
	if (!s || !buf || s->format.mBytesPerFrame == 0) return 1;
	UInt32 numFrames = buf->mAudioDataBytesCapacity / s->format.mBytesPerFrame;
	AudioBufferList abl;
	abl.mNumberBuffers = 1;
	abl.mBuffers[0].mNumberChannels = s->format.mChannelsPerFrame;
	abl.mBuffers[0].mDataByteSize = buf->mAudioDataBytesCapacity;
	abl.mBuffers[0].mData = buf->mAudioData;

	if (ExtAudioFileRead(s->file, &numFrames, &abl) != noErr) {
		numFrames = 0;
	}
	buf->mAudioDataByteSize = numFrames * s->format.mBytesPerFrame;
	if (numFrames > 0 && s->format.mChannelsPerFrame == 2) {
		int eqOn = 0;
		pthread_mutex_lock(&s->eqMu);
		for (int b = 0; b < kEQBands; b++) {
			if (s->eq[b].active) { eqOn = 1; break; }
		}
		if (eqOn) {
			float *pcm = (float *)buf->mAudioData;
			for (UInt32 f = 0; f < numFrames; f++) {
				for (int c = 0; c < 2; c++) {
					double x = pcm[f * 2 + c];
					if (!isfinite(x)) x = 0.0;
					for (int b = 0; b < kEQBands; b++) {
						if (s->eq[b].active) x = vibez_biquad_run(&s->eq[b], x, c);
					}
					if (!isfinite(x)) x = 0.0;
					if (x > 1.0) x = 1.0;
					if (x < -1.0) x = -1.0;
					pcm[f * 2 + c] = (float)x;
				}
			}
		}
		pthread_mutex_unlock(&s->eqMu);
	}
	return numFrames == 0;
}

static void vibez_audio_callback(void *ctx, AudioQueueRef queue, AudioQueueBufferRef buf) {
	vibez_audio_state *s = (vibez_audio_state*)ctx;
	if (__atomic_load_n(&s->done, __ATOMIC_ACQUIRE)) return;
	if (vibez_fill_buffer(s, buf)) {
		__atomic_store_n(&s->done, 1, __ATOMIC_RELEASE);
		__atomic_store_n(&s->playing, 0, __ATOMIC_RELEASE);
		AudioQueueStop(queue, false);
		vibezOnEOS((uintptr_t)s->goPlayer);
		return;
	}
	AudioQueueEnqueueBuffer(queue, buf, 0, NULL);
}

// vibez_open opens a local file path and sets up ExtAudioFile + AudioQueue
// Returning NULL on fail
static vibez_audio_state* vibez_open(const char *uri){
	if (!uri || uri[0] == '\0') return NULL;
	vibez_audio_state *s = (vibez_audio_state*)calloc(1, sizeof(vibez_audio_state));
	if(!s) return NULL;
	pthread_mutex_init(&s->eqMu, NULL);

	CFStringRef cfPath = CFStringCreateWithCString(NULL, uri, kCFStringEncodingUTF8);
	if (!cfPath) {
		pthread_mutex_destroy(&s->eqMu);
		free(s);
		return NULL;
	}
	CFURLRef url = CFURLCreateWithFileSystemPath(NULL, cfPath, kCFURLPOSIXPathStyle, false);
	CFRelease(cfPath);
	if (!url) {
		pthread_mutex_destroy(&s->eqMu);
		free(s);
		return NULL;
	}

	OSStatus err = ExtAudioFileOpenURL(url, &s->file);
	CFRelease(url);
	if(err != noErr || !s->file) {
		pthread_mutex_destroy(&s->eqMu);
		free(s);
		return NULL;
	}

	// Decode to 32-bit float stereo at the file's own sample rate, so a
	// 24-bit or hi-res file reaches the EQ and the queue without being
	// truncated to 16 bits or resampled. CoreAudio converts to the device.
	AudioStreamBasicDescription fileFormat;
	UInt32 size = sizeof(fileFormat);
	double rate = 0;
	if (ExtAudioFileGetProperty(s->file, kExtAudioFileProperty_FileDataFormat, &size, &fileFormat) == noErr) {
		rate = fileFormat.mSampleRate;
	}
	if (!isfinite(rate) || rate < 1.0 || rate > 768000.0) rate = 44100.0;
	s->format.mSampleRate = rate;
	s->format.mFormatID = kAudioFormatLinearPCM;
	s->format.mFormatFlags = kAudioFormatFlagsNativeFloatPacked;
	s->format.mChannelsPerFrame = 2;
	s->format.mBitsPerChannel = 32;
	s->format.mBytesPerFrame = 8;
	s->format.mFramesPerPacket = 1;
	s->format.mBytesPerPacket = 8;
	s->bufFrames = (UInt32)(rate * kBufferSeconds);
	if (s->bufFrames == 0) s->bufFrames = 1;
	if (s->bufFrames > UINT32_MAX / s->format.mBytesPerFrame) {
		ExtAudioFileDispose(s->file);
		pthread_mutex_destroy(&s->eqMu);
		free(s);
		return NULL;
	}

	err = ExtAudioFileSetProperty(s->file, kExtAudioFileProperty_ClientDataFormat, sizeof(s->format), &s->format);
	if(err != noErr){
		ExtAudioFileDispose(s->file);
		pthread_mutex_destroy(&s->eqMu);
		free(s);
		return NULL;
	}

	err = AudioQueueNewOutput(&s->format, vibez_audio_callback, s, NULL, NULL, 0, &s->queue);
	if(err != noErr){
		ExtAudioFileDispose(s->file);
		pthread_mutex_destroy(&s->eqMu);
		free(s);
		return NULL;
	}

	UInt32 bufBytes = s->bufFrames * s->format.mBytesPerFrame;
	for (int i = 0; i < kNumBuffers; i++) {
		if (AudioQueueAllocateBuffer(s->queue, bufBytes, &s->bufs[i]) != noErr || !s->bufs[i]) {
			AudioQueueDispose(s->queue, true);
			ExtAudioFileDispose(s->file);
			pthread_mutex_destroy(&s->eqMu);
			free(s);
			return NULL;
		}
	}
	return s;
}

static void vibez_start(vibez_audio_state *s){
	if (!s || !s->queue) return;
	// Pre filling preallocated buffers before starting.
	for (int i = 0; i < kNumBuffers; i++) {
		if (!s->bufs[i] || vibez_fill_buffer(s, s->bufs[i])) {
			__atomic_store_n(&s->done, 1, __ATOMIC_RELEASE);
			break;
		}
		AudioQueueEnqueueBuffer(s->queue, s->bufs[i], 0, NULL);
	}
	__atomic_store_n(&s->playing, 1, __ATOMIC_RELEASE);
	AudioQueueStart(s->queue, NULL);
}

static void vibez_pause(vibez_audio_state *s){
	if (!s || !s->queue) return;
	__atomic_store_n(&s->playing, 0, __ATOMIC_RELEASE);
	AudioQueuePause(s->queue);
}

static void vibez_resume(vibez_audio_state *s){
	if (!s || !s->queue) return;
	__atomic_store_n(&s->playing, 1, __ATOMIC_RELEASE);
	AudioQueueStart(s->queue, NULL);
}

static void vibez_stop(vibez_audio_state *s){
	if (!s || !s->queue) return;
	__atomic_store_n(&s->playing, 0, __ATOMIC_RELEASE);
	AudioQueueStop(s->queue, true);
}

static void vibez_set_volume(vibez_audio_state *s, float v){
	if (!s || !s->queue) return;
	AudioQueueSetParameter(s->queue, kAudioQueueParam_Volume, v);
}

static SInt64 vibez_get_position(vibez_audio_state *s){
	if (!s || !s->file) return 0;
	SInt64 frame = 0;
	ExtAudioFileTell(s->file, &frame);
	return frame;
}

static double vibez_get_playback_time(vibez_audio_state *s) {
	if (!s || !s->queue || s->format.mSampleRate <= 0) return -1.0;
	AudioTimeStamp ts;
	Boolean discontinuity;
	OSStatus err = AudioQueueGetCurrentTime(s->queue, NULL, &ts, &discontinuity);
	if (err != noErr || !(ts.mFlags & kAudioTimeStampSampleTimeValid)) {
		return -1.0;
	}
	return ts.mSampleTime / s->format.mSampleRate;
}

static void vibez_seek(vibez_audio_state *s, SInt64 frame){
	if (!s || !s->queue) return;
	AudioQueueStop(s->queue, true);
	ExtAudioFileSeek(s->file, frame < 0 ? 0 : frame);
	__atomic_store_n(&s->done, 0, __ATOMIC_RELEASE);
	for (int i = 0; i < kNumBuffers; i++) {
		if (!s->bufs[i] || vibez_fill_buffer(s, s->bufs[i])) {
			__atomic_store_n(&s->done, 1, __ATOMIC_RELEASE);
			break;
		}
		AudioQueueEnqueueBuffer(s->queue, s->bufs[i], 0, NULL);
	}
	if (!__atomic_load_n(&s->done, __ATOMIC_ACQUIRE) && __atomic_load_n(&s->playing, __ATOMIC_ACQUIRE)) {
		AudioQueueStart(s->queue, NULL);
	}
}

static SInt64 vibez_get_duration(vibez_audio_state *s) {
	SInt64 frames = 0;
	UInt32 size = sizeof(frames);
	ExtAudioFileGetProperty(s->file, kExtAudioFileProperty_FileLengthFrames, &size, &frames);
	return frames;
}

static double vibez_get_sample_rate(vibez_audio_state *s) {
	AudioStreamBasicDescription fileFormat;
	UInt32 size = sizeof(fileFormat);
	ExtAudioFileGetProperty(s->file, kExtAudioFileProperty_FileDataFormat, &size, &fileFormat);
		return fileFormat.mSampleRate;
}

static void vibez_destroy(vibez_audio_state *s){
	if(!s) return;
	AudioQueueDispose(s->queue, true);
	ExtAudioFileDispose(s->file);
	pthread_mutex_destroy(&s->eqMu);
	free(s);
}
*/
import "C"

import (
	"fmt"
	"math"
	"runtime/cgo"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/simone-vibes/vibez/internal/audioquality"
	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
)

// audioRef wraps a C audio state with an atomic reference count. It owns the
// cgo handle stored in s->goPlayer, so the handle lives exactly as long as the
// C state that can pass it back through vibezOnEOS.
type audioRef struct {
	s      *C.vibez_audio_state
	p      *Player
	handle cgo.Handle
	refs   int32
}

func newAudioRef(s *C.vibez_audio_state, p *Player) *audioRef {
	r := &audioRef{s: s, p: p, refs: 1}
	r.handle = cgo.NewHandle(r)
	s.goPlayer = C.uintptr_t(r.handle)
	return r
}

// acquire increments the reference count and returns true if the object is still active.
func (r *audioRef) acquire() bool {
	for {
		old := atomic.LoadInt32(&r.refs)
		if old <= 0 {
			return false
		}
		if atomic.CompareAndSwapInt32(&r.refs, old, old+1) {
			return true
		}
	}
}

// release decrements the reference count and destroys the C object when it
// reaches zero. The handle is deleted only after vibez_destroy has disposed of
// the queue, so no callback can still be holding it.
func (r *audioRef) release() {
	if atomic.AddInt32(&r.refs, -1) == 0 {
		C.vibez_destroy(r.s)
		r.handle.Delete()
	}
}

// Player implements player.Player for local audio files using CoreAudio
type Player struct {
	mu         sync.RWMutex
	state      player.State
	bcast      player.Broadcast
	closed     bool
	queue      []provider.Track
	idx        int
	ended      bool
	audio      *audioRef
	sampleRate float64
	done       chan struct{}
	pollWakeCh chan struct{}
	eosCh      chan struct{}
	audioWg    sync.WaitGroup
	allTracks  []provider.Track
	eqGains    []float64
	closeOnce  sync.Once
}

// New creates a local Player backed by CoreAudio
func New(_ func(string)) (*Player, error) {
	p := &Player{
		done:       make(chan struct{}),
		pollWakeCh: make(chan struct{}, 1),
		eosCh:      make(chan struct{}, 1),
	}
	go p.pollState()
	p.audioWg.Add(1)
	go p.eosLoop()
	return p, nil
}

func (p *Player) signalPoll() {
	if p.pollWakeCh == nil {
		return
	}
	select {
	case p.pollWakeCh <- struct{}{}:
	default:
	}
}

func (p *Player) pollState() {
	var ticker *time.Ticker
	var tickC <-chan time.Time
	stopTicker := func() {
		if ticker != nil {
			ticker.Stop()
			ticker = nil
			tickC = nil
		}
	}
	defer stopTicker()

	syncTicker := func() {
		p.mu.RLock()
		playing := p.state.Playing && p.audio != nil
		p.mu.RUnlock()
		if playing {
			if ticker == nil {
				ticker = time.NewTicker(500 * time.Millisecond)
				tickC = ticker.C
			}
		} else {
			stopTicker()
		}
	}

	syncTicker()
	for {
		select {
		case <-p.done:
			return
		case <-p.pollWakeCh:
			syncTicker()
		case <-tickC:
			p.mu.RLock()
			playing := p.state.Playing
			ref := p.audio
			p.mu.RUnlock()
			if !playing || ref == nil {
				stopTicker()
				continue
			}
			if !ref.acquire() {
				continue
			}
			secs := float64(C.vibez_get_playback_time(ref.s))
			ref.release()
			if secs < 0 || math.IsNaN(secs) || math.IsInf(secs, 0) || secs > float64(math.MaxInt64/int64(time.Second)) {
				continue
			}
			pos := time.Duration(secs * float64(time.Second))
			p.mu.Lock()
			p.state.Position = pos
			s := p.state
			p.mu.Unlock()
			p.broadcast(s)
		}
	}
}

func (p *Player) broadcast(s player.State) {
	p.mu.RLock()
	closed := p.closed
	p.mu.RUnlock()
	if closed {
		return
	}
	p.signalPoll()
	p.bcast.Send(s)
}

func (p *Player) eosLoop() {
	defer p.audioWg.Done()
	for {
		select {
		case <-p.eosCh:
			_ = p.Next()
		case <-p.done:
			return
		}
	}
}

//export vibezOnEOS
func vibezOnEOS(h C.uintptr_t) {
	r := cgo.Handle(uintptr(h)).Value().(*audioRef)
	p := r.p
	// A state that has already been swapped out can still reach its end
	// before it is destroyed; that EOS belongs to no track now.
	p.mu.RLock()
	current := p.audio == r
	p.mu.RUnlock()
	if !current {
		return
	}
	select {
	case p.eosCh <- struct{}{}:
	default:
	}
}

func (p *Player) playTrack(t provider.Track) {
	path, ok := strings.CutPrefix(t.ID, "local:")
	if !ok || path == "" {
		p.mu.Lock()
		p.state.Error = fmt.Sprintf("local player: invalid track ID %q", provider.Clean(t.ID))
		s := p.state
		p.mu.Unlock()
		p.broadcast(s)
		return
	}
	cleanPath, err := verifyRegularAudioPath(path)
	if err != nil {
		p.mu.Lock()
		old := p.audio
		p.audio = nil
		p.state.Track = nil
		p.state.Playing = false
		p.state.Position = 0
		p.state.Error = provider.Clean(err.Error())
		s := p.state
		p.mu.Unlock()
		if old != nil {
			old.release()
		}
		p.broadcast(s)
		return
	}

	cs := C.CString(cleanPath)
	defer C.free(unsafe.Pointer(cs))

	raw := C.vibez_open(cs)
	if raw == nil {
		p.mu.Lock()
		p.state.Error = fmt.Sprintf("failed to open: %s", provider.Clean(cleanPath))
		s := p.state
		p.mu.Unlock()
		p.broadcast(s)
		return
	}

	sampleRate := float64(C.vibez_get_sample_rate(raw))
	if math.IsNaN(sampleRate) || math.IsInf(sampleRate, 0) || sampleRate <= 0 || sampleRate > 768000 {
		sampleRate = 44100.0
	}

	frames := int64(C.vibez_get_duration(raw))
	var duration time.Duration
	if frames > 0 {
		sec := float64(frames) / sampleRate
		if !math.IsNaN(sec) && !math.IsInf(sec, 0) && sec > 0 && sec < float64(math.MaxInt64/int64(time.Second)) {
			duration = time.Duration(sec * float64(time.Second))
		}
	}

	ref := newAudioRef(raw, p)

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		ref.release()
		return
	}
	old := p.audio
	p.sampleRate = sampleRate
	p.audio = ref
	p.ended = false
	p.state.Error = ""
	p.state.Track = &t
	p.state.Playing = true
	p.state.Position = 0
	if duration > 0 {
		p.state.Track.Duration = duration
	}
	eqGains := append([]float64(nil), p.eqGains...)
	s := p.state
	p.mu.Unlock()

	if old != nil {
		old.release()
	}
	if !ref.acquire() {
		return
	}
	if len(eqGains) > 0 {
		cg := make([]C.double, len(eqGains))
		for i, g := range eqGains {
			cg[i] = C.double(g)
		}
		C.vibez_set_eq_gains(raw, &cg[0], C.int(len(cg)))
	}
	C.vibez_start(raw)
	ref.release()
	p.broadcast(s)
}

func (p *Player) Play() error {
	p.mu.Lock()
	if (p.ended || p.audio == nil) && len(p.queue) > 0 {
		if p.ended || p.idx < 0 || p.idx >= len(p.queue) {
			p.idx = 0
		}
		p.ended = false
		t := p.queue[p.idx]
		p.mu.Unlock()
		p.playTrack(t)
		return nil
	}
	ref := p.audio
	p.state.Playing = true
	s := p.state
	p.mu.Unlock()
	if ref != nil && ref.acquire() {
		C.vibez_resume(ref.s)
		ref.release()
	}
	p.broadcast(s)
	return nil
}

func (p *Player) Pause() error {
	p.mu.Lock()
	ref := p.audio
	p.state.Playing = false
	s := p.state
	p.mu.Unlock()
	if ref != nil && ref.acquire() {
		C.vibez_pause(ref.s)
		ref.release()
	}
	p.broadcast(s)
	return nil
}

func (p *Player) Stop() error {
	p.mu.Lock()
	ref := p.audio
	p.state.Playing = false
	s := p.state
	p.mu.Unlock()
	if ref != nil && ref.acquire() {
		C.vibez_stop(ref.s)
		ref.release()
	}
	p.broadcast(s)
	return nil
}

func (p *Player) Next() error {
	p.mu.Lock()
	if len(p.queue) == 0 {
		p.mu.Unlock()
		return nil
	}
	if p.idx < 0 || p.idx >= len(p.queue) {
		p.idx = 0
	}
	// RepeatModeOne (restart the current track.)
	if p.state.RepeatMode == player.RepeatModeOne {
		t := p.queue[p.idx]
		p.mu.Unlock()
		p.playTrack(t)
		return nil
	}

	// RepeatModeOff (stop at the end of queue)
	if p.state.RepeatMode == player.RepeatModeOff && p.idx >= len(p.queue)-1 {
		p.ended = true
		p.state.Playing = false
		p.state.Position = 0
		s := p.state
		p.mu.Unlock()
		p.broadcast(s)
		return nil
	}
	p.idx = (p.idx + 1) % len(p.queue)
	t := p.queue[p.idx]
	p.mu.Unlock()
	p.playTrack(t)
	return nil
}

func (p *Player) Previous() error {
	p.mu.Lock()
	if len(p.queue) == 0 {
		p.mu.Unlock()
		return nil
	}
	if p.state.Position > 3*time.Second {
		p.mu.Unlock()
		_ = p.Seek(0)
		return nil
	}
	if p.idx <= 0 || p.idx >= len(p.queue) {
		p.idx = len(p.queue) - 1
	} else {
		p.idx--
	}
	t := p.queue[p.idx]
	p.mu.Unlock()
	p.playTrack(t)
	return nil
}

func (p *Player) Seek(pos time.Duration) error {
	if pos < 0 {
		pos = 0
	}
	p.mu.Lock()
	ref := p.audio
	sr := p.sampleRate
	if math.IsNaN(sr) || math.IsInf(sr, 0) || sr <= 0 {
		sr = 44100.0
	}
	f := pos.Seconds() * sr
	if math.IsNaN(f) || f < 0 {
		f = 0
	} else if maxF := float64(math.MaxInt64 - 1); math.IsInf(f, 1) || f > maxF {
		f = maxF
	}
	frames := C.SInt64(f)
	p.state.Position = pos
	s := p.state
	p.mu.Unlock()
	if ref != nil && ref.acquire() {
		C.vibez_seek(ref.s, frames)
		ref.release()
	}
	p.broadcast(s)
	return nil
}

func (p *Player) SetVolume(v float64) error {
	if math.IsNaN(v) {
		v = 0
	}
	v = min(max(v, 0), 1)
	p.mu.Lock()
	ref := p.audio
	p.state.Volume = v
	s := p.state
	p.mu.Unlock()
	if ref != nil && ref.acquire() {
		C.vibez_set_volume(ref.s, C.float(v))
		ref.release()
	}
	p.broadcast(s)
	return nil
}

func (p *Player) SetAudioBitrate(kbps int) error {
	if err := audioquality.Validate(kbps); err != nil {
		return err
	}
	return player.ErrAudioBitrateSavedPreferenceOnly
}

func (p *Player) SetQueue(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	p.mu.Lock()
	newQueue := tracksForIDs(p.allTracks, ids)
	if len(newQueue) == 0 {
		p.mu.Unlock()
		return nil
	}
	p.queue = newQueue
	p.idx = 0
	t := p.queue[0]
	p.mu.Unlock()
	p.playTrack(t)
	return nil
}

func (p *Player) SetPlaylist(_ string, startIdx int) error {
	p.mu.Lock()
	if len(p.queue) == 0 {
		p.mu.Unlock()
		return nil
	}
	if startIdx >= 0 && startIdx < len(p.queue) {
		p.idx = startIdx
	} else {
		p.idx = 0
	}
	t := p.queue[p.idx]
	p.mu.Unlock()
	p.playTrack(t)
	return nil
}

func (p *Player) AppendQueue(ids []string) error {
	p.mu.Lock()
	extra := tracksForIDs(p.allTracks, ids)
	if len(extra) == 0 {
		p.mu.Unlock()
		return nil
	}
	wasEmpty := len(p.queue) == 0
	wasEnded := p.ended && !p.state.Playing
	firstNewIdx := len(p.queue)
	p.queue = append(p.queue, extra...)
	if wasEmpty {
		p.idx = 0
		p.ended = false
	} else if wasEnded {
		p.idx = firstNewIdx
		p.ended = false
	}
	t := extra[0]
	p.mu.Unlock()
	if wasEmpty || wasEnded {
		p.playTrack(t)
	}
	return nil
}

func (p *Player) SetShuffle(on bool) error {
	p.mu.Lock()
	p.state.ShuffleMode = on
	p.mu.Unlock()
	return nil
}

func (p *Player) SetRepeat(mode int) error {
	p.mu.Lock()
	p.state.RepeatMode = mode
	p.mu.Unlock()
	return nil
}

func (p *Player) SetEqualizer(bands []player.EQBand) error {
	gains := make([]float64, len(bands))
	for i, b := range bands {
		if math.IsNaN(b.Gain) || math.IsInf(b.Gain, 0) {
			continue
		}
		g := b.Gain
		if g > 24 {
			g = 24
		} else if g < -24 {
			g = -24
		}
		gains[i] = g
	}
	p.mu.Lock()
	p.eqGains = gains
	ref := p.audio
	p.mu.Unlock()
	if ref != nil && ref.acquire() {
		if len(gains) == 0 {
			C.vibez_set_eq_gains(ref.s, nil, 0)
		} else {
			cg := make([]C.double, len(gains))
			for i, g := range gains {
				cg[i] = C.double(g)
			}
			C.vibez_set_eq_gains(ref.s, &cg[0], C.int(len(cg)))
		}
		ref.release()
	}
	return nil
}

func (p *Player) RemoveFromQueue(idx int) error {
	p.mu.Lock()
	var old *audioRef
	if idx >= 0 && idx < len(p.queue) {
		p.queue = append(p.queue[:idx], p.queue[idx+1:]...)
		switch {
		case idx == p.idx:
			p.state.Track = nil
			p.state.Playing = false
			p.state.Position = 0
			old = p.audio
			p.audio = nil
			if len(p.queue) == 0 || p.idx >= len(p.queue) {
				p.idx = 0
			}
		case idx < p.idx:
			p.idx--
		}
	}
	s := p.state
	p.mu.Unlock()
	if old != nil {
		old.release()
	}
	p.broadcast(s)
	return nil
}

func (p *Player) MoveInQueue(from, to int) error {
	p.mu.Lock()
	if from >= 0 && from < len(p.queue) && to >= 0 && to < len(p.queue) {
		t := p.queue[from]
		p.queue = append(p.queue[:from], p.queue[from+1:]...)
		if from < to {
			to--
		}
		p.queue = append(p.queue[:to], append([]provider.Track{t}, p.queue[to:]...)...)
		switch {
		case from == p.idx:
			p.idx = to
		case from < p.idx && to >= p.idx:
			p.idx--
		case from > p.idx && to <= p.idx:
			p.idx++
		}
	}
	p.mu.Unlock()
	return nil
}

func (p *Player) ClearQueue() error {
	p.mu.Lock()
	old := p.audio
	p.audio = nil
	p.queue = nil
	p.idx = 0
	p.state.Track = nil
	p.state.Playing = false
	s := p.state
	p.mu.Unlock()
	if old != nil {
		old.release()
	}
	p.broadcast(s)
	return nil
}

func (p *Player) GetState() (*player.State, error) {
	p.mu.Lock()
	ref := p.audio
	p.mu.Unlock()
	if ref != nil && ref.acquire() {
		secs := float64(C.vibez_get_playback_time(ref.s))
		ref.release()
		if secs >= 0 && !math.IsNaN(secs) && !math.IsInf(secs, 0) && secs <= float64(math.MaxInt64/int64(time.Second)) {
			p.mu.Lock()
			p.state.Position = time.Duration(secs * float64(time.Second))
			p.mu.Unlock()
		}
	}
	p.mu.RLock()
	s := p.state
	p.mu.RUnlock()
	return &s, nil
}

func (p *Player) Subscribe() <-chan player.State {
	return p.bcast.Subscribe()
}

func (p *Player) Close() error {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		p.mu.Unlock()
		close(p.done)
		p.bcast.Close()
		p.audioWg.Wait()
		p.mu.Lock()
		old := p.audio
		p.audio = nil
		p.mu.Unlock()
		if old != nil {
			old.release()
		}
	})
	return nil
}

func (p *Player) LoadTracks(tracks []provider.Track) {
	p.mu.Lock()
	p.allTracks = append([]provider.Track{}, tracks...)
	p.queue = append([]provider.Track{}, tracks...)
	p.idx = 0
	p.mu.Unlock()
}

func tracksForIDs(tracks []provider.Track, ids []string) []provider.Track {
	byID := make(map[string]provider.Track, len(tracks))
	for _, t := range tracks {
		byID[t.ID] = t
	}
	out := make([]provider.Track, 0, len(ids))
	for _, id := range ids {
		if t, ok := byID[id]; ok {
			out = append(out, t)
		}
	}
	return out
}
