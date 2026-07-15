package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// callRecorderBuf accumulates peer-side PCM16 samples for the duration of
// a single call. It is intentionally simple: a slice guarded by a mutex,
// flushed on call end. The recording covers ONLY the peer leg (the bot's
// TTS playback is generated server-side and not re-captured here).
type callRecorderBuf struct {
	mu         sync.Mutex
	pcm        []float32
	sampleRate int
}

func newCallRecorderBuf(sampleRate int) *callRecorderBuf {
	if sampleRate <= 0 {
		sampleRate = 16000
	}
	return &callRecorderBuf{sampleRate: sampleRate}
}

func (b *callRecorderBuf) write(pcm []float32) {
	if b == nil {
		return
	}
	b.mu.Lock()
	// Hard cap: ~30 minutes at 16 kHz mono float32 (~115 MB) — calls longer
	// than that simply stop growing to protect memory.
	const maxSamples = 30 * 60 * 16000
	room := maxSamples - len(b.pcm)
	if room <= 0 {
		b.mu.Unlock()
		return
	}
	if len(pcm) > room {
		pcm = pcm[:room]
	}
	b.pcm = append(b.pcm, pcm...)
	b.mu.Unlock()
}

// flush writes the buffered audio to disk under media/recordings/ and
// returns the relative path (suitable for callStore.RecordingInfo.Path),
// the mime type, and the byte size.
func (b *callRecorderBuf) flush(sessionID, callID string) (string, string, int64, error) {
	if b == nil {
		return "", "", 0, fmt.Errorf("nil recorder")
	}
	b.mu.Lock()
	pcm := b.pcm
	rate := b.sampleRate
	b.pcm = nil
	b.mu.Unlock()
	if len(pcm) < rate/2 {
		return "", "", 0, fmt.Errorf("recording too short")
	}
	dir := filepath.Join("media", "recordings")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", 0, err
	}
	name := fmt.Sprintf("call_%s_%s_%d.wav", sessionID, callID, time.Now().UnixMilli())
	full := filepath.Join(dir, name)
	data := encodeWavMonoPCM16(pcm, rate)
	if err := os.WriteFile(full, data, 0o644); err != nil {
		return "", "", 0, err
	}
	return filepath.Join("recordings", name), "audio/wav", int64(len(data)), nil
}

// persistCallRecording flushes the buffer and registers the recording on
// the call_records row. Best-effort; errors are returned but the caller
// is expected to log-and-continue (call teardown must never block).
func persistCallRecording(ctx context.Context, store *callStore, sessionID, callID string, buf *callRecorderBuf) (RecordingInfo, error) {
	if store == nil || buf == nil {
		return RecordingInfo{}, fmt.Errorf("missing store/buffer")
	}
	relPath, mime, size, err := buf.flush(sessionID, callID)
	if err != nil {
		return RecordingInfo{}, err
	}
	token, err := newShareToken()
	if err != nil {
		// Recording still on disk; we just won't have a share token.
		token = ""
	}
	info := RecordingInfo{
		CallID:   callID,
		Path:     relPath,
		Mime:     mime,
		Size:     size,
		Token:    token,
		Uploaded: time.Now().UnixMilli(),
	}
	if err := store.SetRecording(ctx, callID, info); err != nil {
		return info, err
	}
	return info, nil
}
