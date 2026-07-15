package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// holdMusicPlayer streams the configured hold-music WAV into a live call in
// real time, looping until Stop() is called or the call ends. Uses ffmpeg to
// decode any input format to PCM16 mono 16 kHz, then feeds ~500ms chunks into
// the CallManager capture ring, keeping the audio path in sync with the send
// loop.

type holdMusicPlayer struct {
	cancel context.CancelFunc
	done   chan struct{}
}

var (
	holdPlayersMu sync.Mutex
	holdPlayers   = map[string]*holdMusicPlayer{} // key = sid+"/"+callID
)

// startHoldMusic begins looping the configured hold-music for scope into the
// given call. If a player already exists for this call it's replaced. Volume
// is applied linearly per-sample (0..2). Missing/invalid file is a no-op.
func startHoldMusic(sess *Session, callID, scope string) {
	if sess == nil || callID == "" {
		return
	}
	stopHoldMusic(sess.id, callID)

	path := holdMusicPath(scope)
	if scope != "global" {
		if _, err := os.Stat(path); err != nil {
			// Fallback to global if queue-specific file is missing.
			path = holdMusicPath("global")
		}
	}
	if _, err := os.Stat(path); err != nil {
		return
	}
	meta := readHoldMeta(scope)
	if meta.Volume == 0 {
		meta.Volume = 100
	}
	gain := float32(meta.Volume) / 100.0
	if gain <= 0 {
		gain = 1
	}
	if gain > 4 {
		gain = 4
	}

	ctx, cancel := context.WithCancel(context.Background())
	p := &holdMusicPlayer{cancel: cancel, done: make(chan struct{})}

	holdPlayersMu.Lock()
	holdPlayers[holdKey(sess.id, callID)] = p
	holdPlayersMu.Unlock()

	go func() {
		defer close(p.done)
		defer func() {
			holdPlayersMu.Lock()
			if cur, ok := holdPlayers[holdKey(sess.id, callID)]; ok && cur == p {
				delete(holdPlayers, holdKey(sess.id, callID))
			}
			holdPlayersMu.Unlock()
		}()

		// Decode once, then loop the buffer in real time. Keeps CPU low
		// and avoids re-spawning ffmpeg every loop.
		pcm, err := decodeToMono16k(ctx, path)
		if err != nil || len(pcm) == 0 {
			return
		}
		if gain != 1 {
			for i := range pcm {
				v := pcm[i] * gain
				if v > 1 {
					v = 1
				} else if v < -1 {
					v = -1
				}
				pcm[i] = v
			}
		}

		const sampleRate = 16000
		const chunkSamples = sampleRate / 2 // 500ms
		chunkDur := time.Duration(chunkSamples) * time.Second / time.Duration(sampleRate)

		i := 0
		ticker := time.NewTicker(chunkDur)
		defer ticker.Stop()
		for {
			// Confirm call still active and hold still active before feeding.
			if sess.reg == nil {
				return
			}
			ac, ok := sess.reg.get(callID)
			if !ok {
				return
			}
			st := getHold(sess.id, callID)
			if !st.OnHold {
				return
			}
			end := i + chunkSamples
			var chunk []float32
			if end <= len(pcm) {
				chunk = pcm[i:end]
				i = end
				if i >= len(pcm) {
					i = 0
				}
			} else {
				chunk = append([]float32{}, pcm[i:]...)
				remaining := chunkSamples - len(chunk)
				if remaining > 0 && remaining <= len(pcm) {
					chunk = append(chunk, pcm[:remaining]...)
					i = remaining
				} else {
					i = 0
				}
			}
			ac.cm.FeedCapturedPCM(chunk)

			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// stopHoldMusic signals the player (if any) to stop and waits briefly for the
// goroutine to exit so a subsequent start does not race with the previous one.
func stopHoldMusic(sessID, callID string) {
	holdPlayersMu.Lock()
	p := holdPlayers[holdKey(sessID, callID)]
	if p != nil {
		delete(holdPlayers, holdKey(sessID, callID))
	}
	holdPlayersMu.Unlock()
	if p == nil {
		return
	}
	p.cancel()
	select {
	case <-p.done:
	case <-time.After(500 * time.Millisecond):
	}
}

func decodeToMono16k(ctx context.Context, path string) ([]float32, error) {
	cmd := exec.CommandContext(ctx, "ffmpeg", "-loglevel", "error", "-i", path,
		"-vn", "-ac", "1", "-ar", "16000", "-f", "s16le", "-")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(stdout)
	waitErr := cmd.Wait()
	if readErr != nil && readErr != io.EOF {
		return nil, fmt.Errorf("ffmpeg read: %w", readErr)
	}
	if waitErr != nil && len(data) == 0 {
		return nil, fmt.Errorf("ffmpeg: %w", waitErr)
	}
	return pcm16BytesToFloat32(data), nil
}
