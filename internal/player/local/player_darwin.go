//go:build darwin

// Package local provides a Player backed by CoreAudio for local audio files.
// It uses ExtAudioFile for format-agnostic decoding and AudioQueue for
// PCM output. Both frameworks ship with every macOS installation.
package local

/*
#cgo LDFLAGS: -framework AudioToolbox -framework CoreFoundation
#include <AudioToolbox/AudioToolbox.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>
#include <string.h>

// the buffers in the queue
#define kNumBuffers 3
// Buffer size 32KB per buffer (in bytes)
#define kBufferSize 32768

typedef struct {
	AudioQueueRef queue;
	ExtAudioFileRef file;
	AudioStreamBasicDescription format;
	int done;
	uintptr_t goPlayer;
} vibez_audio_state;

// This is called from C back into GO when the track ends
extern void vibezOnEOS(uintptr_t handle);

// forward declaration
static void vibez_audio_callback(void* ctx, AudioQueueRef queue, AudioQueueBufferRef buf);

// vibez_fill_buffer reads decoded PCM from ExtAudioFile into an AudioQueue buffer.
// Returns 1 if EOS was reached, 0 otherwise.
static int vibez_fill_buffer(vibez_audio_state *s, AudioQueueBufferRef buf) {
	UInt32 numFrames = buf->mAudioDataBytesCapacity / s->format.mBytesPerFrame;
	AudioBufferList abl;
	abl.mNumberBuffers = 1;
	abl.mBuffers[0].mNumberChannels = s->format.mChannelsPerFrame;
	abl.mBuffers[0].mDataByteSize = buf->mAudioDataBytesCapacity;
	abl.mBuffers[0].mData = buf->mAudioData;

	ExtAudioFileRead(s->file, &numFrames, &abl);
	buf->mAudioDataByteSize = numFrames * s->format.mBytesPerFrame;
	return numFrames == 0;
}

static void vibez_audio_callback(void *ctx, AudioQueueRef queue, AudioQueueBufferRef buf) {
	vibez_audio_state *s = (vibez_audio_state*)ctx;
	if(s->done) return;
	if(vibez_fill_buffer(s, buf)) {
		s->done = 1;
		AudioQueueStop(queue, false);
		vibezOnEOS((uintptr_t)s->goPlayer);
		return;
	}
	AudioQueueEnqueueBuffer(queue, buf, 0, NULL);
}

// vibez_open opens a file:// URI and sets up ExtAudioFile + AudioQueue
// Returning NULL on fail
static vibez_audio_state* vibez_open(const char *uri){
	vibez_audio_state *s = (vibez_audio_state*)calloc(1, sizeof(vibez_audio_state));
	if(!s) return NULL;

	CFStringRef cfPath = CFStringCreateWithCString(NULL, uri, kCFStringEncodingUTF8);
	CFURLRef url = CFURLCreateWithFileSystemPath(NULL, cfPath, kCFURLPOSIXPathStyle, false);
	CFRelease(cfPath);

	OSStatus err = ExtAudioFileOpenURL(url, &s->file);
	CFRelease(url);
	if(err != noErr) {free(s); return NULL;}

    // Request signed 16-bit stereo PCM at 44100 Hz output.
	s->format.mSampleRate = 44100.0;
	s->format.mFormatID = kAudioFormatLinearPCM;
	s->format.mFormatFlags = kAudioFormatFlagIsSignedInteger | kAudioFormatFlagIsPacked;
	s->format.mChannelsPerFrame = 2;
	s->format.mBitsPerChannel = 16;
	s->format.mBytesPerFrame = 4;
	s->format.mFramesPerPacket = 1;
	s->format.mBytesPerPacket = 4;

	ExtAudioFileSetProperty(s->file, kExtAudioFileProperty_ClientDataFormat, sizeof(s->format), &s->format);

	err = AudioQueueNewOutput(&s->format, vibez_audio_callback, s, NULL, NULL, 0, &s->queue);
	if(err != noErr){
		ExtAudioFileDispose(s->file);
		free(s);
		return NULL;
	}
	return s;
}

static void vibez_start(vibez_audio_state *s){
	// Pre filling all buffers before starting
	for(int i=0; i<kNumBuffers; i++){
		AudioQueueBufferRef buf;
		AudioQueueAllocateBuffer(s->queue, kBufferSize, &buf);
		if(vibez_fill_buffer(s, buf)){
			s->done = 1;
			break;
		}
		AudioQueueEnqueueBuffer(s->queue, buf, 0, NULL);
	}
	AudioQueueStart(s->queue, NULL);
}

static void vibez_pause(vibez_audio_state *s){
	AudioQueuePause(s->queue);
}

static void vibez_resume(vibez_audio_state *s){
	AudioQueueStart(s->queue, NULL);
}

static void vibez_stop(vibez_audio_state *s){
	AudioQueueStop(s->queue, true);
}

static void vibez_set_volume(vibez_audio_state *s, float v){
	AudioQueueSetParameter(s->queue, kAudioQueueParam_Volume, v);
}

static SInt64 vibez_get_position(vibez_audio_state *s){
	SInt64 frame = 0;
	ExtAudioFileTell(s->file, &frame);
	return frame;
}

static void vibez_seek(vibez_audio_state *s, SInt64 frame){
	AudioQueueStop(s->queue, true);
	ExtAudioFileSeek(s->file, frame);
	AudioQueueStart(s->queue, NULL);
}

static SInt64 vibez_get_duration(vibez_audio_state *s) {
	SInt64 frames = 0;
	UInt32 size = sizeof(frames);
	ExtAudioFileGetProperty(s->file, kExtAudioFileProperty_FileLengthFrames, &size, &frames);
	return frames;
}

static void vibez_destroy(vibez_audio_state *s){
	if(!s) return;
	AudioQueueDispose(s->queue, true);
	ExtAudioFileDispose(s->file);
	free(s);
}
*/
import "C"

import (
	"fmt"
	"runtime/cgo"
	"sync"
	"time"
	"unsafe"

	"github.com/simone-vibes/vibez/internal/audioquality"
	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
)

// Player implements player.Player for local audio files using CoreAudio
type Player struct {
	mu     sync.RWMutex
	state  player.State
	subs   []chan player.State
	queue  []provider.Track
	idx    int
	audio  *C.vibez_audio_state
	done   chan struct{}
	eosCh  chan struct{}
	handle cgo.Handle
}

// New creates a local Player backed by CoreAudio
func New() (*Player, error) {
	p := &Player{
		done:  make(chan struct{}),
		eosCh: make(chan struct{}, 1),
	}
	go p.pollState()
	go p.eosLoop()
	return p, nil
}

func (p *Player) pollState() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			p.mu.RLock()
			playing := p.state.Playing
			p.mu.RUnlock()
			if !playing {
				continue
			}
			p.mu.Lock()
			s := p.state
			p.mu.Unlock()
			p.broadcast(s)
		case <-p.done:
			return
		}
	}
}

func (p *Player) broadcast(s player.State) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, ch := range p.subs {
		select {
		case ch <- s:
		default:
		}
	}
}

func (p *Player) eosLoop() {
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
func vibezOnEOS(ptr unsafe.Pointer) {
	p := cgo.Handle(uintptr(h)).Value().(*Player)
	select {
	case p.eosCh <- struct{}{}:
	default:
	}
}

func (p *Player) playTrack(t provider.Track) {
	// Stripping the "local:" prefix to get the raw fle path
	path := t.ID[len("local:"):]
	uri := path

	p.mu.Lock()

	if p.audio != nil {
		C.vibez_destroy(p.audio)
		p.audio = nil
	}
	p.mu.Unlock()

	cs := C.CString(uri)
	defer C.free(unsafe.Pointer(cs))

	audio := C.vibez_open(cs)
	if audio == nil {
		p.mu.Lock()
		p.state.Error = fmt.Sprintf("failed to open: %s", path)
		s := p.state
		p.mu.Unlock()
		p.broadcast(s)
		return
	}

	if p.handle != 0 {
		p.handle.Delete()
	}
	p.handle = cgo.NewHandle(p)
	audio.goPlayer = C.uintptr_t(p.handle)
	C.vibez_start(audio)

	// Read duration
	frames := int64(C.vibez_get_duration(audio))
	duration := time.Duration(frames) * time.Second / 44100

	p.mu.Lock()
	p.audio = audio
	p.state.Track = &t
	p.state.Playing = true
	p.state.Position = 0
	if duration > 0 {
		p.state.Track.Duration = duration
	}
	s := p.state
	p.mu.Unlock()
	p.broadcast(s)
}

func (p *Player) Play() error {
	p.mu.Lock()
	if p.audio != nil {
		C.vibez_resume(p.audio)
	}
	p.state.Playing = true
	s := p.state
	p.mu.Unlock()
	p.broadcast(s)
	return nil
}

func (p *Player) Pause() error {
	p.mu.Lock()
	if p.audio != nil {
		C.vibez_pause(p.audio)
	}
	p.state.Playing = false
	s := p.state
	p.mu.Unlock()
	p.broadcast(s)
	return nil
}

func (p *Player) Stop() error {
	p.mu.Lock()
	if p.audio != nil {
		C.vibez_stop(p.audio)
	}
	p.state.Playing = false
	s := p.state
	p.mu.Unlock()
	p.broadcast(s)
	return nil
}

func (p *Player) Next() error {
	p.mu.Lock()
	if len(p.queue) == 0 {
		p.mu.Unlock()
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
	if p.idx == 0 {
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
	p.mu.Lock()
	if p.audio != nil {
		frames := C.SInt64(pos.Seconds() * 44100)
		C.vibez_seek(p.audio, frames)
	}
	p.state.Position = pos
	s := p.state
	p.mu.Unlock()
	p.broadcast(s)
	return nil
}

func (p *Player) SetVolume(v float64) error {
	p.mu.Lock()
	if p.audio != nil {
		C.vibez_set_volume(p.audio, C.float(v))
	}
	p.state.Volume = v
	s := p.state
	p.mu.Unlock()
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
	found := false
	for i, t := range p.queue {
		if t.ID == ids[0] {
			p.idx = i
			found = true
			break
		}
	}
	if !found || len(p.queue) == 0 {
		p.mu.Unlock()
		return nil
	}
	t := p.queue[p.idx]
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
	extra := tracksForIDs(p.queue, ids)
	p.mu.Lock()
	p.queue = append(p.queue, extra...)
	p.mu.Unlock()
	return nil
}

func (p *Player) SetRepeat(mode int) error {
	p.mu.Lock()
	p.state.RepeatMode = mode
	p.mu.Unlock()
	return nil
}

func (p *Player) SetEqualizer(_ []player.EQBand) error { return nil }

func (p *Player) RemoveFromQueue(idx int) error {
	p.mu.Lock()
	if idx >= 0 && idx < len(p.queue) {
		p.queue = append(p.queue[:idx], p.queue[idx+1:]...)
		switch {
		case idx == p.idx:
			p.state.Track = nil
			p.state.Playing = false
			if p.audio != nil {
				C.vibez_stop(p.audio)
			}
		case idx < p.idx:
			p.idx--
		}
	}
	s := p.state
	p.mu.Unlock()
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
	if p.audio != nil {
		C.vibez_stop(p.audio)
		C.vibez_destroy(p.audio)
		p.audio = nil
	}
	p.queue = nil
	p.idx = 0
	p.state.Track = nil
	p.state.Playing = false
	s := p.state
	p.mu.Unlock()
	p.broadcast(s)
	return nil
}

func (p *Player) GetState() (*player.State, error) {
	p.mu.Lock()
	if p.audio != nil {
		frames := int64(C.vibez_get_position(p.audio))
		p.state.Position = time.Duration(frames) * time.Second / 44100
	}
	s := p.state
	p.mu.Unlock()
	return &s, nil
}

func (p *Player) Subscribe() <-chan player.State {
	ch := make(chan player.State, 8)
	p.mu.Lock()
	p.subs = append(p.subs, ch)
	p.mu.Unlock()
	return ch
}

func (p *Player) Close() error {
	close(p.done)
	p.mu.Lock()
	if p.audio != nil {
		C.vibez_destroy(p.audio)
		p.audio = nil
	}
	if p.handle != 0 {
		p.handle.Delete()
		p.handle = 0
	}
	p.mu.Unlock()
	return nil
}

func (p *Player) LoadTracks(tracks []provider.Track) {
	p.mu.Lock()
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
