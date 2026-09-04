package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
	"wacalls/internal/voip/core"
)

// FlowBridge is the glue between the FlowExecutor and the active call/session
// state. It implements the side-effects of voice_menu, message and
// whatsapp_send nodes against the running call.
type FlowBridge struct {
	mgr    *SessionManager
	log    *slog.Logger
	broker *Broker
	tracer *flowTracer

	httpClient *http.Client

	// Configurable via environment so deployments can plug their preferred
	// providers without code changes.
	ttsURL   string
	ttsAuth  string
	ttsVoice string
	sttURL   string
	sttAuth  string
	sttLang  string
}

func newFlowBridge(mgr *SessionManager, log *slog.Logger) *FlowBridge {
	ttsURL := strings.TrimSpace(os.Getenv("WACALLS_TTS_URL"))
	if ttsURL == "" {
		// The installer ships Piper locally through piper-bridge. Defaulting here
		// avoids a muted URA when an older service unit is missing the env var.
		ttsURL = "http://127.0.0.1:5005/api/tts"
	}
	return &FlowBridge{
		mgr:        mgr,
		log:        log,
		httpClient: &http.Client{Timeout: 60 * time.Second},
		ttsURL:     ttsURL,
		ttsAuth:    os.Getenv("WACALLS_TTS_AUTH"),
		ttsVoice:   os.Getenv("WACALLS_TTS_VOICE"),
		sttURL:     os.Getenv("WACALLS_STT_URL"),
		sttAuth:    os.Getenv("WACALLS_STT_AUTH"),
		sttLang:    os.Getenv("WACALLS_STT_LANG"),
	}
}

// AttachBroker lets the bridge surface diagnostic events to the user (e.g.
// "TTS não configurado") so the URA failing silently becomes obvious.
func (b *FlowBridge) AttachBroker(br *Broker) {
	if b == nil {
		return
	}
	b.broker = br
}

// AttachTracer wires the per-call correlation tracer so TTS/STT failures
// surface under the same traceId as the executor steps.
func (b *FlowBridge) AttachTracer(t *flowTracer) {
	if b == nil {
		return
	}
	b.tracer = t
}

func (b *FlowBridge) emitFlowSkip(sessionID, callID, reason, detail string) {
	traceID := ""
	if b != nil && b.tracer != nil {
		traceID = b.tracer.TraceIDFor(callID)
		b.tracer.Record(FlowTraceStep{
			CallID: callID, SessionID: sessionID, TraceID: traceID,
			Level: "error", Code: "tts", Message: reason,
			Data: map[string]any{"detail": detail},
		})
	}
	if b == nil || b.broker == nil {
		return
	}
	b.broker.deliverScoped(sessionID, map[string]any{
		"type":      "flow-skip",
		"sessionId": sessionID,
		"callId":    callID,
		"reason":    reason,
		"detail":    detail,
		"traceId":   traceID,
		"ts":        time.Now().UnixMilli(),
	})
}

// TTSConfigured reports whether at least the default Piper-style endpoint is
// configured. Per-flow ElevenLabs voices override this, but a healthy default
// keeps inbound URAs working when the flow doesn't set its own voice.
func (b *FlowBridge) TTSConfigured() bool {
	if b == nil {
		return false
	}
	return strings.TrimSpace(b.ttsURL) != ""
}

// STTConfigured reports whether speech-to-text is wired up. Voice menu nodes
// still work with DTMF when this is false; only free-speech capture breaks.
func (b *FlowBridge) STTConfigured() bool {
	if b == nil {
		return false
	}
	return strings.TrimSpace(b.sttURL) != ""
}

// ---- TTS ---------------------------------------------------------------

// PlayTTS synthesizes `text` via the configured TTS endpoint and injects the
// resulting audio into the active call. It blocks for the duration of the
// audio playback (or until ctx is cancelled / the call ends).
func (b *FlowBridge) PlayTTS(ctx context.Context, sessionID, callID, text string) error {
	return b.PlayTTSVoice(ctx, sessionID, callID, text, "")
}

// PlayTTSVoice is PlayTTS with an explicit voice override. When voice == ""
// the default voice configured via WACALLS_TTS_VOICE is used.
func (b *FlowBridge) PlayTTSVoice(ctx context.Context, sessionID, callID, text, voice string) error {
	return b.PlayTTSConfig(ctx, sessionID, callID, text, voiceConfigForNode(nil, voice))
}

// PlayTTSConfig speaks using the voice saved inside the flow graph. This is
// what makes the FlowBuilder "Voz do fluxo" dialog affect real inbound calls
// on the VPS, instead of always falling back to WACALLS_TTS_* defaults.
func (b *FlowBridge) PlayTTSConfig(ctx context.Context, sessionID, callID, text string, cfg *FlowVoiceConfig) error {
	if b == nil {
		return errors.New("flow bridge not initialised")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	sess, ok := b.mgr.Get(sessionID)
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	ac, ok := sess.reg.get(callID)
	if !ok {
		return fmt.Errorf("call %s not active", callID)
	}
	ttsCtx := context.Background()
	pcm, err := b.synthTTSConfig(ttsCtx, text, cfg)
	if err != nil {
		// Surface the most common cause (no TTS endpoint) directly to the
		// UI so the operator knows why the caller heard silence.
		reason := "tts_failed"
		if strings.Contains(err.Error(), "WACALLS_TTS_URL not configured") {
			reason = "tts_not_configured"
		}
		b.emitFlowSkip(sessionID, callID, reason, err.Error())
		return fmt.Errorf("tts: %w", err)
	}
	if err := waitForCallMediaReady(ttsCtx, ac, mediaReadyTimeout()); err != nil {
		b.emitFlowSkip(sessionID, callID, "media_not_ready", err.Error())
		return err
	}
	// FeedCapturedPCM expects mono 16 kHz float32, range -1..1.
	//
	// Push the FULL synthesized PCM into the call's capture buffer in a
	// single call and let the media send loop (the only RTP clock) pace
	// it out frame-by-frame. The previous implementation paced the feed
	// here in 60ms steps as well, which created a dual-clock race with
	// the send loop: whenever the two clocks drifted out of phase the
	// send loop briefly starved and emitted a silence frame, which is
	// exactly the "picotado/tremido" chop the user reported on URA and
	// FlowBuilder voice prompts. With a single clock the audio plays
	// smoothly even under load.
	ac.cm.FeedCapturedPCM(pcm)
	// Mirror into the recorder in one shot for the same reason.
	if rec := ac.fullRecorder; rec != nil {
		mirror := make([]float32, len(pcm))
		copy(mirror, pcm)
		rec.write(mirror)
	}
	const sampleRate = 16000
	playDur := time.Duration(len(pcm)) * time.Second / time.Duration(sampleRate)
	// Add a small tail (one frame) so the last 60ms isn't cut off when the
	// caller chains another action right after PlayTTS returns.
	playDur += 80 * time.Millisecond
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(playDur):
	}
	return nil
}

// TransferCallToQueue routes the ongoing URA call to a chat queue (and
// optionally to a specific operator). It:
//  1. Plays the optional spoken prompt ("Transferindo para o suporte…").
//  2. Writes the chat metadata (status = waiting/open, queueId/userId set)
//     so the conversation appears in the queue's panel for an agent.
//  3. Logs a "transferred" lifecycle event on the chat timeline.
//  4. Ends the call so the customer drops cleanly into chat.
//
// dryRun=true skips the side-effects (used by /api/flows/test).
func (b *FlowBridge) TransferCallToQueue(ctx context.Context, sessionID, callID, queueID, userID, prompt string, voiceCfg *FlowVoiceConfig) error {
	if b == nil || b.mgr == nil {
		return errors.New("flow bridge not initialised")
	}
	sess, ok := b.mgr.Get(sessionID)
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	ac, ok := sess.reg.get(callID)
	if !ok {
		return fmt.Errorf("call %s not active", callID)
	}
	peer := strings.TrimSpace(ac.peer)
	if peer == "" {
		return fmt.Errorf("call %s has no peer jid", callID)
	}
	// 1. Speak the prompt while still on the call.
	if msg := strings.TrimSpace(prompt); msg != "" {
		if err := b.PlayTTSConfig(ctx, sessionID, callID, msg, voiceCfg); err != nil {
			// Surface the failure but continue with the transfer so the
			// customer isn't stuck on a dead call.
			b.log.Warn("transfer prompt tts failed", "err", err, "call_id", callID)
		}
	}
	// 2. Persist the chat assignment.
	if b.mgr.chatMeta != nil {
		status := ChatStatusWaiting
		if strings.TrimSpace(userID) != "" {
			status = ChatStatusOpen
		}
		now := time.Now().UnixMilli()
		if err := b.mgr.chatMeta.SetAssignment(ctx, sessionID, peer, status, strings.TrimSpace(userID), strings.TrimSpace(queueID), now); err != nil {
			b.log.Error("transfer assignment failed", "err", err, "call_id", callID, "queue", queueID)
			return err
		}
		detail := strings.TrimSpace(userID)
		if q := strings.TrimSpace(queueID); q != "" {
			if detail != "" {
				detail += " · "
			}
			detail += "queue=" + q
		}
		kind := "transferred"
		if strings.TrimSpace(userID) == "" {
			kind = "requeued"
		}
		if b.broker != nil {
			logChatEvent(ctx, b.mgr.chatMeta, b.broker, sessionID, peer, kind, "", "URA", detail, now)
		}
		if m, ok2, _ := b.mgr.chatMeta.Get(ctx, sessionID, peer); ok2 && b.broker != nil {
			b.broker.emitChatMeta(m)
		}
	}
	// 3. End the call so the conversation hands off to the queue chat.
	_ = ac.cm.EndCall(context.Background(), core.EndCallReasonUserEnded)
	return nil
}

func (b *FlowBridge) synthTTSConfig(ctx context.Context, text string, cfg *FlowVoiceConfig) ([]float32, error) {
	provider := "piper"
	if cfg != nil && strings.TrimSpace(cfg.Provider) != "" {
		provider = strings.ToLower(strings.TrimSpace(cfg.Provider))
	}
	switch provider {
	case "elevenlabs":
		return b.synthElevenLabsTTS(ctx, text, cfg)
	case "openai":
		return b.synthOpenAITTS(ctx, text, cfg)
	}
	voice := ""
	if cfg != nil {
		voice = strings.TrimSpace(cfg.VoiceID)
	}
	type ttsResult struct {
		pcm []float32
		err error
	}
	ch := make(chan ttsResult, 1)
	go func() {
		pcm, err := b.synthTTS(ctx, text, voice)
		ch <- ttsResult{pcm: pcm, err: err}
	}()
	var err error
	select {
	case res := <-ch:
		if res.err == nil {
			return res.pcm, nil
		}
		err = res.err
	case <-time.After(piperGraceTimeout()):
		err = fmt.Errorf("Piper TTS não respondeu em %s", piperGraceTimeout())
		if alt, ferr := b.synthOfflineFallbackTTS(ctx, text); ferr == nil && len(alt) > 0 {
			if b.log != nil {
				b.log.Warn("Piper TTS slow; used offline espeak fallback", "err", err)
			}
			return alt, nil
		}
		if alt, ferr := b.synthConfiguredFallbackTTS(ctx, text, cfg); ferr == nil && len(alt) > 0 {
			if b.log != nil {
				b.log.Warn("Piper TTS slow; used cloud fallback", "err", err)
			}
			return alt, nil
		}
		select {
		case res := <-ch:
			if res.err == nil {
				return res.pcm, nil
			}
			err = res.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if alt, ferr := b.synthOfflineFallbackTTS(ctx, text); ferr == nil && len(alt) > 0 {
		if b.log != nil {
			b.log.Warn("Piper TTS failed; used offline espeak fallback", "err", err)
		}
		return alt, nil
	}
	if alt, ferr := b.synthConfiguredFallbackTTS(ctx, text, cfg); ferr == nil && len(alt) > 0 {
		if b.log != nil {
			b.log.Warn("Piper TTS failed; used cloud fallback", "err", err)
		}
		return alt, nil
	}
	return nil, err
}

// synthTTS calls the configured TTS HTTP endpoint and returns mono PCM
// float32 samples at 16 kHz. The endpoint contract is intentionally simple:
//
//	POST {WACALLS_TTS_URL}
//	Authorization: {WACALLS_TTS_AUTH}  (optional)
//	Body: {"text": "...", "voice": "..."}
//	Response: raw 16-bit little-endian PCM, mono, 16 kHz (audio/L16)
//
// This shape matches what a thin wrapper around ElevenLabs / Piper / Coqui
// typically exposes. When no endpoint is configured the call returns an
// error so the executor can record it without crashing the flow.
func (b *FlowBridge) synthTTS(ctx context.Context, text, voice string) ([]float32, error) {
	if b.ttsURL == "" {
		return nil, errors.New("WACALLS_TTS_URL not configured")
	}
	if voice == "" {
		voice = b.ttsVoice
	}
	payload, _ := json.Marshal(map[string]string{
		"text":  text,
		"voice": voice,
	})
	if cached, ok := readTTSCache("piper", voice, text); ok {
		return cached, nil
	}
	// Do not inherit short-lived signaling/request cancellation here. Some
	// WhatsApp call contexts are cancelled right after AcceptCall/answer events;
	// TTS must keep running under its own hard deadline or the URA never speaks.
	ttsCtx, cancel := context.WithTimeout(context.Background(), ttsRequestTimeout())
	defer cancel()
	req, err := http.NewRequestWithContext(ttsCtx, http.MethodPost, b.ttsURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "audio/L16; rate=16000; channels=1")
	if b.ttsAuth != "" {
		req.Header.Set("Authorization", b.ttsAuth)
	}
	client := &http.Client{Timeout: ttsRequestTimeout() + 5*time.Second}
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "Client.Timeout") || strings.Contains(err.Error(), "context deadline exceeded") {
			return nil, fmt.Errorf("Piper TTS demorou mais de %s para responder em %s. O sistema tentará a voz offline/cloud se disponível; rode a atualização para instalar o fallback local: %w", ttsRequestTimeout().String(), b.ttsURL, err)
		}
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("tts http %d: %s", resp.StatusCode, string(body))
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 50*1024*1024))
	if err != nil {
		return nil, err
	}
	pcm := pcm16BytesToFloat32(raw)
	writeTTSCache("piper", voice, text, pcm)
	return pcm, nil
}

func ttsRequestTimeout() time.Duration {
	if raw := strings.TrimSpace(os.Getenv("WACALLS_TTS_TIMEOUT_SECONDS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 30 && n <= 600 {
			return time.Duration(n) * time.Second
		}
	}
	return 35 * time.Second
}

func piperGraceTimeout() time.Duration {
	if raw := strings.TrimSpace(os.Getenv("WACALLS_TTS_GRACE_SECONDS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 1 && n <= 30 {
			return time.Duration(n) * time.Second
		}
	}
	return 4 * time.Second
}

func (b *FlowBridge) synthOfflineFallbackTTS(ctx context.Context, text string) ([]float32, error) {
	voice := strings.TrimSpace(os.Getenv("WACALLS_ESPEAK_VOICE"))
	if voice == "" {
		voice = "pt-br"
	}
	speed := strings.TrimSpace(os.Getenv("WACALLS_ESPEAK_SPEED"))
	if speed == "" {
		speed = "145"
	}
	cacheVoice := voice + ":" + speed
	if cached, ok := readTTSCache("espeak", cacheVoice, text); ok {
		return cached, nil
	}
	bin, err := exec.LookPath("espeak-ng")
	if err != nil {
		return nil, errors.New("fallback offline espeak-ng não instalado; rode a atualização do instalador")
	}
	tmp, err := os.CreateTemp("", "wacalls-tts-*.txt")
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()
	_, _ = tmp.WriteString(text)
	_ = tmp.Close()
	defer os.Remove(tmpName)

	tctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(tctx, bin, "-v", voice, "-s", speed, "--stdout", "-f", tmpName)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	wav, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, errors.New(msg)
	}
	pcm16, err := decodeAudioToPCM16(tctx, wav, "wav")
	if err != nil {
		return nil, fmt.Errorf("converter espeak-ng para PCM: %w", err)
	}
	pcm := pcm16BytesToFloat32(pcm16)
	writeTTSCache("espeak", cacheVoice, text, pcm)
	return pcm, nil
}

func (b *FlowBridge) synthElevenLabsTTS(ctx context.Context, text string, cfg *FlowVoiceConfig) ([]float32, error) {
	apiKey := strings.TrimSpace(cfg.ElevenLabsAPIKey)
	voiceID := strings.TrimSpace(cfg.VoiceID)
	if apiKey == "" {
		return nil, errors.New("ElevenLabs API key não salva na voz do fluxo")
	}
	if voiceID == "" {
		return nil, errors.New("ElevenLabs voiceId não selecionado na voz do fluxo")
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = "eleven_flash_v2_5"
	}
	body, _ := json.Marshal(map[string]any{
		"text":     text,
		"model_id": model,
	})
	url := fmt.Sprintf("https://api.elevenlabs.io/v1/text-to-speech/%s?output_format=mp3_44100_128", voiceID)
	ttsCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ttsCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("xi-api-key", apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("elevenlabs http %d: %s", resp.StatusCode, string(raw))
	}
	mp3, err := io.ReadAll(io.LimitReader(resp.Body, 50*1024*1024))
	if err != nil {
		return nil, err
	}
	pcm, err := decodeAudioToPCM16(ttsCtx, mp3, "mp3")
	if err != nil {
		return nil, fmt.Errorf("converter ElevenLabs para PCM: %w", err)
	}
	return pcm16BytesToFloat32(pcm), nil
}

func (b *FlowBridge) synthConfiguredFallbackTTS(ctx context.Context, text string, cfg *FlowVoiceConfig) ([]float32, error) {
	if strings.TrimSpace(os.Getenv("OPENAI_API_KEY")) != "" {
		return b.synthOpenAITTS(ctx, text, cfg)
	}
	return nil, errors.New("nenhum fallback TTS configurado")
}

func (b *FlowBridge) synthOpenAITTS(ctx context.Context, text string, cfg *FlowVoiceConfig) ([]float32, error) {
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if apiKey == "" {
		return nil, errors.New("OPENAI_API_KEY não configurada")
	}
	voice := cloudVoice(cfg)
	model := cloudModel(cfg, "gpt-4o-mini-tts")
	return b.synthOpenAICompatibleTTS(ctx, "openai", "https://api.openai.com/v1/audio/speech", apiKey, model, voice, text)
}

func (b *FlowBridge) synthOpenAICompatibleTTS(ctx context.Context, provider, url, apiKey, model, voice, text string) ([]float32, error) {
	cacheVoice := voice + ":" + model
	if cached, ok := readTTSCache(provider, cacheVoice, text); ok {
		return cached, nil
	}
	body, _ := json.Marshal(map[string]any{
		"model":           model,
		"input":           text,
		"voice":           voice,
		"response_format": "mp3",
	})
	ttsCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ttsCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("%s tts http %d: %s", provider, resp.StatusCode, string(raw))
	}
	mp3, err := io.ReadAll(io.LimitReader(resp.Body, 50*1024*1024))
	if err != nil {
		return nil, err
	}
	pcm16, err := decodeAudioToPCM16(ttsCtx, mp3, "mp3")
	if err != nil {
		return nil, fmt.Errorf("converter %s para PCM: %w", provider, err)
	}
	pcm := pcm16BytesToFloat32(pcm16)
	writeTTSCache(provider, cacheVoice, text, pcm)
	return pcm, nil
}

func cloudVoice(cfg *FlowVoiceConfig) string {
	if cfg != nil && strings.TrimSpace(cfg.VoiceID) != "" {
		return strings.TrimSpace(cfg.VoiceID)
	}
	if v := strings.TrimSpace(os.Getenv("WACALLS_CLOUD_TTS_VOICE")); v != "" {
		return v
	}
	return "alloy"
}

func cloudModel(cfg *FlowVoiceConfig, def string) string {
	if cfg != nil && strings.TrimSpace(cfg.Model) != "" {
		return strings.TrimSpace(cfg.Model)
	}
	return def
}

func mediaReadyTimeout() time.Duration {
	if raw := strings.TrimSpace(os.Getenv("WACALLS_MEDIA_READY_TIMEOUT_SECONDS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 1 && n <= 60 {
			return time.Duration(n) * time.Second
		}
	}
	return 20 * time.Second
}

func waitForCallMediaReady(ctx context.Context, ac *activeCall, timeout time.Duration) error {
	if ac == nil || ac.cm == nil {
		return errors.New("chamada não encontrada para áudio")
	}
	if ac.cm.MediaReady() {
		return nil
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			if ac.cm.MediaReady() {
				return nil
			}
			return fmt.Errorf("mídia da chamada não conectou em %s; WhatsApp aceitou, mas o relay de áudio ainda não abriu", timeout)
		case <-tick.C:
			if ac.cm.MediaReady() {
				return nil
			}
		}
	}
}

func ttsCachePath(provider, voice, text string) string {
	h := sha1.Sum([]byte(provider + "\x00" + voice + "\x00" + text))
	return filepath.Join("media", "tts-cache", hex.EncodeToString(h[:])+".pcm")
}

func readTTSCache(provider, voice, text string) ([]float32, bool) {
	if strings.TrimSpace(text) == "" {
		return nil, false
	}
	raw, err := os.ReadFile(ttsCachePath(provider, voice, text))
	if err != nil || len(raw) == 0 {
		return nil, false
	}
	return pcm16BytesToFloat32(raw), true
}

func writeTTSCache(provider, voice, text string, pcm []float32) {
	if len(pcm) == 0 || strings.TrimSpace(text) == "" {
		return
	}
	path := ttsCachePath(provider, voice, text)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, encodePCM16Raw(pcm), 0o644)
}

func encodePCM16Raw(pcm []float32) []byte {
	buf := bytes.NewBuffer(make([]byte, 0, len(pcm)*2))
	for _, s := range pcm {
		if s > 1 {
			s = 1
		} else if s < -1 {
			s = -1
		}
		_ = binary.Write(buf, binary.LittleEndian, int16(s*32767))
	}
	return buf.Bytes()
}

func decodeAudioToPCM16(ctx context.Context, raw []byte, inputFormat string) ([]byte, error) {
	bin, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, errors.New("ffmpeg não instalado; rode o instalador/atualização na VPS para habilitar ElevenLabs nas chamadas")
	}
	cctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	args := []string{"-loglevel", "error"}
	if inputFormat != "" {
		args = append(args, "-f", inputFormat)
	}
	args = append(args, "-i", "pipe:0", "-vn", "-ac", "1", "-ar", "16000", "-f", "s16le", "pipe:1")
	cmd := exec.CommandContext(cctx, bin, args...)
	cmd.Stdin = bytes.NewReader(raw)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, errors.New(msg)
	}
	return out, nil
}

// ---- STT (record peer audio + transcribe) ------------------------------

// RecordAndTranscribe attaches a flow audio sink to the active call, buffers
// the peer's audio for up to maxSec seconds (cutting earlier on a simple
// energy-based end-of-speech detector), and returns the transcript produced
// by the configured STT endpoint. Returns an empty string when no STT
// endpoint is configured.
func (b *FlowBridge) RecordAndTranscribe(ctx context.Context, sessionID, callID string, maxSec int) (string, error) {
	if b == nil {
		return "", errors.New("flow bridge not initialised")
	}
	if maxSec <= 0 {
		maxSec = 5
	}
	sess, ok := b.mgr.Get(sessionID)
	if !ok {
		return "", fmt.Errorf("session %s not found", sessionID)
	}
	ac, ok := sess.reg.get(callID)
	if !ok {
		return "", fmt.Errorf("call %s not active", callID)
	}

	const sampleRate = 16000
	maxSamples := sampleRate * maxSec
	buf := make([]float32, 0, maxSamples)

	// Simple voice-activity detection: cut after ~800 ms of low energy
	// once we have heard at least ~400 ms of speech.
	const silenceWindow = sampleRate * 800 / 1000
	const minSpeech = sampleRate * 400 / 1000
	// Telephony audio transcoded from G.711 ends up quieter than 0.02
	// RMS in float32, so the old threshold left URAs deaf — the VAD
	// never flipped "heardSpeech" and the buffer was discarded as
	// silence. 0.006 matches typical PSTN levels while still rejecting
	// line noise.
	const speechThresh = 0.006

	done := make(chan string, 1)
	var heardSpeech bool
	var silentRun int

	sink := func(pcm []float32) {
		// Append (bounded)
		room := maxSamples - len(buf)
		if room <= 0 {
			return
		}
		if len(pcm) > room {
			pcm = pcm[:room]
		}
		buf = append(buf, pcm...)

		// Energy check
		var sumSq float64
		for _, s := range pcm {
			sumSq += float64(s) * float64(s)
		}
		rms := math.Sqrt(sumSq / float64(len(pcm)+1))
		if rms >= speechThresh {
			heardSpeech = true
			silentRun = 0
		} else {
			silentRun += len(pcm)
		}

		if (heardSpeech && silentRun >= silenceWindow && len(buf) >= minSpeech) || len(buf) >= maxSamples {
			select {
			case done <- "":
			default:
			}
		}
	}
	sess.reg.setFlowAudioSink(callID, sink)
	_ = ac // keep reference clear

	defer sess.reg.setFlowAudioSink(callID, nil)

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-done:
	case <-time.After(time.Duration(maxSec) * time.Second):
	}

	if b.sttURL == "" {
		return "", nil
	}
	if len(buf) < sampleRate/10 {
		return "", nil
	}
	return b.transcribe(ctx, buf, sampleRate)
}

// ---- Real-time DTMF (RFC2833-equivalent via in-band Goertzel) -----------
//
// Telephony G.711 audio carries DTMF tones in-band as two superimposed sine
// waves (697/770/852/941 Hz × 1209/1336/1477/1633 Hz). We detect them on the
// PCM stream coming out of the call so the URA can react the instant the
// caller presses a key — no STT round-trip, no perceived delay.

var dtmfRowFreqs = [4]float64{697, 770, 852, 941}
var dtmfColFreqs = [4]float64{1209, 1336, 1477, 1633}
var dtmfKeypad = [4][4]byte{
	{'1', '2', '3', 'A'},
	{'4', '5', '6', 'B'},
	{'7', '8', '9', 'C'},
	{'*', '0', '#', 'D'},
}

// goertzelMag returns the magnitude of `freq` in `samples` at sampleRate.
func goertzelMag(samples []float32, sampleRate, freq float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	k := math.Round(float64(len(samples)) * freq / sampleRate)
	w := 2.0 * math.Pi * k / float64(len(samples))
	cosw := math.Cos(w)
	coeff := 2.0 * cosw
	var q0, q1, q2 float64
	for _, s := range samples {
		q0 = coeff*q1 - q2 + float64(s)
		q2 = q1
		q1 = q0
	}
	return math.Sqrt(q1*q1 + q2*q2 - q1*q2*coeff)
}

// detectDTMF returns the keypad character present in the audio frame, or 0.
// Uses a relative threshold: the strongest row & col must be well above the
// other frequencies in their group to count as a valid tone.
func detectDTMF(frame []float32, sampleRate int) byte {
	return detectDTMFWithThreshold(frame, sampleRate, 8.0, 1.8)
}

func detectDTMFWithThreshold(frame []float32, sampleRate int, minMag, dominance float64) byte {
	if len(frame) < sampleRate/100 { // need ~10ms
		return 0
	}
	sr := float64(sampleRate)
	var rowMag [4]float64
	var colMag [4]float64
	for i, f := range dtmfRowFreqs {
		rowMag[i] = goertzelMag(frame, sr, f)
	}
	for i, f := range dtmfColFreqs {
		colMag[i] = goertzelMag(frame, sr, f)
	}
	rIdx, rMax := 0, rowMag[0]
	for i, m := range rowMag {
		if m > rMax {
			rIdx, rMax = i, m
		}
	}
	cIdx, cMax := 0, colMag[0]
	for i, m := range colMag {
		if m > cMax {
			cIdx, cMax = i, m
		}
	}
	// Energy must be meaningful and the winners must dominate the others
	// by a clear margin to avoid false positives on voice.
	var rOther, cOther float64
	for i, m := range rowMag {
		if i != rIdx && m > rOther {
			rOther = m
		}
	}
	for i, m := range colMag {
		if i != cIdx && m > cOther {
			cOther = m
		}
	}
	if minMag <= 0 {
		minMag = 8.0
	}
	if dominance <= 0 {
		dominance = 1.8
	}
	if rMax < minMag || cMax < minMag {
		return 0
	}
	if rMax < rOther*dominance || cMax < cOther*dominance {
		return 0
	}
	return dtmfKeypad[rIdx][cIdx]
}

type AudioDetectConfig struct {
	SilenceMs         int
	MinSpeechMs       int
	SpeechThreshold   float64
	DisableDTMF       bool
	DTMFWindowMs      int
	DTMFStableWindows int
	DTMFMinMagnitude  float64
	DTMFDominance     float64
}

// RecordDTMFOrTranscribe attaches a single audio sink that simultaneously
// detects in-band DTMF digits AND buffers PCM for STT. The instant a DTMF
// digit is detected (confirmed across ~2 consecutive 40ms windows), the
// function returns (digit, "", nil) without waiting for STT — that is what
// makes "DTMF na hora" responsive. Otherwise, it behaves like
// RecordAndTranscribe and returns ("", transcript, err) once speech ends.
// matchSet, when non-empty, restricts which characters trigger early exit.
func (b *FlowBridge) RecordDTMFOrTranscribe(ctx context.Context, sessionID, callID string, maxSec int, matchSet string) (digit string, transcript string, err error) {
	return b.RecordDTMFOrTranscribeConfig(ctx, sessionID, callID, maxSec, matchSet, AudioDetectConfig{})
}

func (b *FlowBridge) RecordDTMFOrTranscribeConfig(ctx context.Context, sessionID, callID string, maxSec int, matchSet string, cfg AudioDetectConfig) (digit string, transcript string, err error) {
	if b == nil {
		return "", "", errors.New("flow bridge not initialised")
	}
	if maxSec <= 0 {
		maxSec = 6
	}
	sess, ok := b.mgr.Get(sessionID)
	if !ok {
		return "", "", fmt.Errorf("session %s not found", sessionID)
	}
	const sampleRate = 16000
	if cfg.DTMFWindowMs <= 0 {
		cfg.DTMFWindowMs = 40
	}
	if cfg.DTMFWindowMs < 20 {
		cfg.DTMFWindowMs = 20
	}
	if cfg.DTMFWindowMs > 80 {
		cfg.DTMFWindowMs = 80
	}
	if cfg.DTMFStableWindows <= 0 {
		cfg.DTMFStableWindows = 2
	}
	if cfg.SilenceMs <= 0 {
		cfg.SilenceMs = 800
	}
	if cfg.SilenceMs < 300 {
		cfg.SilenceMs = 300
	}
	if cfg.MinSpeechMs <= 0 {
		cfg.MinSpeechMs = 300
	}
	if cfg.SpeechThreshold <= 0 {
		cfg.SpeechThreshold = 0.006
	}
	windowSamples := sampleRate * cfg.DTMFWindowMs / 1000
	silenceWindow := sampleRate * cfg.SilenceMs / 1000
	minSpeech := sampleRate * cfg.MinSpeechMs / 1000
	maxSamples := sampleRate * maxSec
	buf := make([]float32, 0, maxSamples)
	window := make([]float32, 0, windowSamples)
	type result struct {
		digit byte
	}
	done := make(chan result, 1)
	var heardSpeech bool
	var silentRun int
	var lastDigit byte
	var stableCount int
	sink := func(pcm []float32) {
		// Append to speech buffer (bounded)
		room := maxSamples - len(buf)
		if room > 0 {
			take := pcm
			if len(take) > room {
				take = take[:room]
			}
			buf = append(buf, take...)
		}
		// VAD energy on this packet
		var sumSq float64
		for _, s := range pcm {
			sumSq += float64(s) * float64(s)
		}
		rms := math.Sqrt(sumSq / float64(len(pcm)+1))
		if rms >= cfg.SpeechThreshold {
			heardSpeech = true
			silentRun = 0
		} else {
			silentRun += len(pcm)
		}
		if !cfg.DisableDTMF {
			// DTMF detection in configurable Goertzel windows
			for _, s := range pcm {
				window = append(window, s)
				if len(window) < windowSamples {
					continue
				}
				d := detectDTMFWithThreshold(window, sampleRate, cfg.DTMFMinMagnitude, cfg.DTMFDominance)
				window = window[:0]
				if d == 0 {
					lastDigit = 0
					stableCount = 0
					continue
				}
				if matchSet != "" && !strings.ContainsRune(matchSet, rune(d)) {
					continue
				}
				if d == lastDigit {
					stableCount++
				} else {
					lastDigit = d
					stableCount = 1
				}
				if stableCount >= cfg.DTMFStableWindows {
					select {
					case done <- result{digit: d}:
					default:
					}
					return
				}
			}
		}
		if (heardSpeech && silentRun >= silenceWindow && len(buf) >= minSpeech) || len(buf) >= maxSamples {
			select {
			case done <- result{}:
			default:
			}
		}
	}
	sess.reg.setFlowAudioSink(callID, sink)
	defer sess.reg.setFlowAudioSink(callID, nil)
	var res result
	select {
	case <-ctx.Done():
		return "", "", ctx.Err()
	case res = <-done:
	case <-time.After(time.Duration(maxSec) * time.Second):
	}
	if res.digit != 0 {
		return string(res.digit), "", nil
	}
	if b.sttURL == "" {
		return "", "", nil
	}
	if len(buf) < sampleRate/10 {
		return "", "", nil
	}
	t, e := b.transcribe(ctx, buf, sampleRate)
	return "", t, e
}

// transcribe POSTs a WAV-encoded clip to the configured STT endpoint and
// returns the recognised text. Expected response: {"text": "..."}.
func (b *FlowBridge) transcribe(ctx context.Context, pcm []float32, rate int) (string, error) {
	wav := encodeWavMonoPCM16(pcm, rate)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.sttURL, bytes.NewReader(wav))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "audio/wav")
	if b.sttAuth != "" {
		req.Header.Set("Authorization", b.sttAuth)
	}
	if b.sttLang != "" {
		req.Header.Set("X-Language", b.sttLang)
	}
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("stt http %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.Text), nil
}

// ---- WhatsApp ----------------------------------------------------------

// SendWhatsAppText delivers a plain text WhatsApp message via the session's
// whatsmeow client. `to` may be a bare phone number (digits) or a full JID.
func (b *FlowBridge) SendWhatsAppText(ctx context.Context, sessionID, to, text string) error {
	if b == nil {
		return errors.New("flow bridge not initialised")
	}
	if to == "" || text == "" {
		return errors.New("whatsapp_send: missing destination or message")
	}
	sess, ok := b.mgr.Get(sessionID)
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	jid, err := parseRecipientJID(to)
	if err != nil {
		return err
	}
	msg := &waE2E.Message{Conversation: proto.String(text)}
	resp, err := sess.client.SendMessage(ctx, jid, msg)
	if err != nil {
		return err
	}
	b.recordOutgoingChatMessage(ctx, sess, jid, string(resp.ID), resp.Timestamp.UnixMilli(), "text", text)
	return nil
}

func whatsappInteractiveBizNodes() []waBinary.Node {
	return []waBinary.Node{{
		Tag: "biz",
		Content: []waBinary.Node{{
			Tag:   "interactive",
			Attrs: waBinary.Attrs{"type": "native_flow", "v": "1"},
			Content: []waBinary.Node{{
				Tag:   "native_flow",
				Attrs: waBinary.Attrs{"v": "9", "name": "mixed"},
			}},
		}},
	}}
}

func (b *FlowBridge) recordOutgoingChatMessage(ctx context.Context, sess *Session, jid types.JID, id string, ts int64, kind, body string) {
	if b == nil || b.mgr == nil || b.mgr.messages == nil || sess == nil || id == "" {
		return
	}
	if ts <= 0 {
		ts = time.Now().UnixMilli()
	}
	row := MessageRow{
		ID: id, SessionID: sess.id, ChatJID: jid.String(), SenderJID: jidOrEmpty(sess),
		FromMe: true, Ts: ts, Kind: kind, Body: body,
	}
	_ = b.mgr.messages.Insert(ctx, row)
}

func interactiveButtonsBody(body, footer string, buttons []FlowButton) string {
	p := map[string]interface{}{"__type": "wa_interactive", "variant": "buttons", "body": body, "footer": footer, "buttons": buttons}
	b, _ := json.Marshal(p)
	return string(b)
}

func interactiveListBody(body, footer, buttonText string, sections []FlowListSection) string {
	p := map[string]interface{}{"__type": "wa_interactive", "variant": "list", "body": body, "footer": footer, "buttonText": buttonText, "sections": sections}
	b, _ := json.Marshal(p)
	return string(b)
}

// FlowButton is a single quick-reply button rendered natively by WhatsApp.
type FlowButton struct {
	ID    string
	Title string
}

// FlowListRow is one selectable row inside a list section.
type FlowListRow struct {
	ID          string
	Title       string
	Description string
}

// FlowListSection groups list rows under a header.
type FlowListSection struct {
	Title string
	Rows  []FlowListRow
}

// SendWhatsAppButtons sends an InteractiveMessage with native quick-reply
// buttons. WhatsApp mobile renders these as real tappable buttons.
func (b *FlowBridge) SendWhatsAppButtons(ctx context.Context, sessionID, to, body, footer string, buttons []FlowButton) error {
	if b == nil {
		return errors.New("flow bridge not initialised")
	}
	if to == "" || body == "" || len(buttons) == 0 {
		return errors.New("whatsapp_buttons: missing destination/body/buttons")
	}
	sess, ok := b.mgr.Get(sessionID)
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	jid, err := parseRecipientJID(to)
	if err != nil {
		return err
	}
	nfb := make([]*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton, 0, len(buttons))
	for i, bt := range buttons {
		id := bt.ID
		if id == "" {
			id = fmt.Sprintf("%d", i+1)
		}
		params := fmt.Sprintf(`{"display_text":%q,"id":%q}`, bt.Title, id)
		nfb = append(nfb, &waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{
			Name:             proto.String("quick_reply"),
			ButtonParamsJSON: proto.String(params),
		})
	}
	im := &waE2E.InteractiveMessage{
		Body: &waE2E.InteractiveMessage_Body{Text: proto.String(body)},
		InteractiveMessage: &waE2E.InteractiveMessage_NativeFlowMessage_{
			NativeFlowMessage: &waE2E.InteractiveMessage_NativeFlowMessage{Buttons: nfb, MessageVersion: proto.Int32(3)},
		},
	}
	if footer != "" {
		im.Footer = &waE2E.InteractiveMessage_Footer{Text: proto.String(footer)}
	}
	msg := &waE2E.Message{
		ViewOnceMessage: &waE2E.FutureProofMessage{
			Message: &waE2E.Message{InteractiveMessage: im},
		},
	}
	nodes := whatsappInteractiveBizNodes()
	resp, err := sess.client.SendMessage(ctx, jid, msg, whatsmeow.SendRequestExtra{AdditionalNodes: &nodes})
	if err != nil {
		// Fallback to plain text so the contact still sees something.
		lines := []string{body}
		for i, bt := range buttons {
			lines = append(lines, fmt.Sprintf("[ %d ] %s", i+1, bt.Title))
		}
		if footer != "" {
			lines = append(lines, "", footer)
		}
		return b.SendWhatsAppText(ctx, sessionID, to, strings.Join(lines, "\n"))
	}
	b.recordOutgoingChatMessage(ctx, sess, jid, string(resp.ID), resp.Timestamp.UnixMilli(), "text", interactiveButtonsBody(body, footer, buttons))
	return nil
}

// SendWhatsAppList sends an InteractiveMessage with a native single-select
// list. Mobile WhatsApp renders this as a tappable list.
func (b *FlowBridge) SendWhatsAppList(ctx context.Context, sessionID, to, body, footer, buttonText string, sections []FlowListSection) error {
	if b == nil {
		return errors.New("flow bridge not initialised")
	}
	if to == "" || body == "" || len(sections) == 0 {
		return errors.New("whatsapp_list: missing destination/body/sections")
	}
	sess, ok := b.mgr.Get(sessionID)
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	jid, err := parseRecipientJID(to)
	if err != nil {
		return err
	}
	if buttonText == "" {
		buttonText = "Selecionar"
	}
	// Build params JSON for single_select
	type lRow struct {
		Header      string `json:"header,omitempty"`
		Title       string `json:"title"`
		Description string `json:"description,omitempty"`
		ID          string `json:"id"`
	}
	type lSec struct {
		Title string `json:"title"`
		Rows  []lRow `json:"rows"`
	}
	out := struct {
		Title    string `json:"title"`
		Sections []lSec `json:"sections"`
	}{Title: buttonText}
	idx := 0
	for _, sec := range sections {
		ls := lSec{Title: sec.Title}
		for _, r := range sec.Rows {
			idx++
			id := r.ID
			if id == "" {
				id = fmt.Sprintf("%d", idx)
			}
			ls.Rows = append(ls.Rows, lRow{Title: r.Title, Description: r.Description, ID: id})
		}
		out.Sections = append(out.Sections, ls)
	}
	paramsBytes, _ := json.Marshal(out)
	im := &waE2E.InteractiveMessage{
		Body: &waE2E.InteractiveMessage_Body{Text: proto.String(body)},
		InteractiveMessage: &waE2E.InteractiveMessage_NativeFlowMessage_{
			NativeFlowMessage: &waE2E.InteractiveMessage_NativeFlowMessage{
				Buttons: []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{{
					Name:             proto.String("single_select"),
					ButtonParamsJSON: proto.String(string(paramsBytes)),
				}},
				MessageVersion: proto.Int32(3),
			},
		},
	}
	if footer != "" {
		im.Footer = &waE2E.InteractiveMessage_Footer{Text: proto.String(footer)}
	}
	msg := &waE2E.Message{
		ViewOnceMessage: &waE2E.FutureProofMessage{
			Message: &waE2E.Message{InteractiveMessage: im},
		},
	}
	nodes := whatsappInteractiveBizNodes()
	resp, err := sess.client.SendMessage(ctx, jid, msg, whatsmeow.SendRequestExtra{AdditionalNodes: &nodes})
	if err != nil {
		// Text fallback
		lines := []string{body}
		n := 0
		for _, sec := range sections {
			if sec.Title != "" {
				lines = append(lines, "", "*"+sec.Title+"*")
			}
			for _, r := range sec.Rows {
				n++
				line := fmt.Sprintf("[ %d ] %s", n, r.Title)
				if r.Description != "" {
					line += " — " + r.Description
				}
				lines = append(lines, line)
			}
		}
		if footer != "" {
			lines = append(lines, "", footer)
		}
		return b.SendWhatsAppText(ctx, sessionID, to, strings.Join(lines, "\n"))
	}
	b.recordOutgoingChatMessage(ctx, sess, jid, string(resp.ID), resp.Timestamp.UnixMilli(), "text", interactiveListBody(body, footer, buttonText, sections))
	return nil
}

// RecordPeerAudio captures up to `seconds` of the peer's audio and writes it
// to disk as a 16 kHz mono WAV file. Returns a path (relative URL) under
// /api/media/recordings/... that the static media handler serves.
func (b *FlowBridge) RecordPeerAudio(ctx context.Context, sessionID, callID string, seconds int) (string, error) {
	if b == nil {
		return "", errors.New("flow bridge not initialised")
	}
	if seconds <= 0 {
		seconds = 5
	}
	sess, ok := b.mgr.Get(sessionID)
	if !ok {
		return "", fmt.Errorf("session %s not found", sessionID)
	}
	if _, ok := sess.reg.get(callID); !ok {
		return "", fmt.Errorf("call %s not active", callID)
	}
	const sampleRate = 16000
	maxSamples := sampleRate * seconds
	buf := make([]float32, 0, maxSamples)
	done := make(chan struct{}, 1)
	sess.reg.setFlowAudioSink(callID, func(pcm []float32) {
		room := maxSamples - len(buf)
		if room <= 0 {
			select {
			case done <- struct{}{}:
			default:
			}
			return
		}
		if len(pcm) > room {
			pcm = pcm[:room]
		}
		buf = append(buf, pcm...)
		if len(buf) >= maxSamples {
			select {
			case done <- struct{}{}:
			default:
			}
		}
	})
	defer sess.reg.setFlowAudioSink(callID, nil)
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-done:
	case <-time.After(time.Duration(seconds) * time.Second):
	}
	if len(buf) < sampleRate/10 {
		return "", errors.New("recording too short")
	}
	dir := filepath.Join("media", "recordings")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s_%s_%d.wav", sessionID, callID, time.Now().UnixMilli())
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, encodeWavMonoPCM16(buf, sampleRate), 0o644); err != nil {
		return "", err
	}
	return "/api/media/recordings/" + name, nil
}

// SendWhatsAppMedia fetches `mediaURL` over HTTP and sends it as an image,
// audio, video, or document to `to`. `caption` is used for image/video,
// `filename` is used for document.
func (b *FlowBridge) SendWhatsAppMedia(ctx context.Context, sessionID, to, kind, mediaURL, caption, filename string) error {
	if b == nil {
		return errors.New("flow bridge not initialised")
	}
	if to == "" || mediaURL == "" {
		return errors.New("whatsapp_media: missing destination or URL")
	}
	sess, ok := b.mgr.Get(sessionID)
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	jid, err := parseRecipientJID(to)
	if err != nil {
		return err
	}
	// Resolve same-origin /api/media/... URLs locally to avoid a self HTTP fetch.
	var data []byte
	var mime string
	if strings.HasPrefix(mediaURL, "/api/media/") {
		rel := strings.TrimPrefix(mediaURL, "/api/media/")
		full := filepath.Join("media", filepath.FromSlash(rel))
		data, err = os.ReadFile(full)
		if err != nil {
			return fmt.Errorf("read local media: %w", err)
		}
	} else {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, mediaURL, nil)
		if err != nil {
			return err
		}
		resp, err := b.httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			return fmt.Errorf("fetch media http %d", resp.StatusCode)
		}
		mime = resp.Header.Get("Content-Type")
		data, err = io.ReadAll(io.LimitReader(resp.Body, 64*1024*1024))
		if err != nil {
			return err
		}
	}
	var appInfo whatsmeow.MediaType
	switch strings.ToLower(kind) {
	case "audio":
		appInfo = whatsmeow.MediaAudio
	case "video":
		appInfo = whatsmeow.MediaVideo
	case "document":
		appInfo = whatsmeow.MediaDocument
	default:
		appInfo = whatsmeow.MediaImage
	}
	up, err := sess.client.Upload(ctx, data, appInfo)
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	if mime == "" {
		mime = guessMimeForKind(kind)
	}
	msg := buildMediaMessage(kind, up, mime, caption, filename, uint64(len(data)))
	if _, err := sess.client.SendMessage(ctx, jid, msg); err != nil {
		return err
	}
	return nil
}

func buildMediaMessage(kind string, up whatsmeow.UploadResponse, mime, caption, filename string, size uint64) *waE2E.Message {
	switch strings.ToLower(kind) {
	case "audio":
		return &waE2E.Message{AudioMessage: &waE2E.AudioMessage{
			URL:           proto.String(up.URL),
			DirectPath:    proto.String(up.DirectPath),
			MediaKey:      up.MediaKey,
			FileEncSHA256: up.FileEncSHA256,
			FileSHA256:    up.FileSHA256,
			FileLength:    proto.Uint64(size),
			Mimetype:      proto.String(mime),
			PTT:           proto.Bool(true),
		}}
	case "video":
		return &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
			URL:           proto.String(up.URL),
			DirectPath:    proto.String(up.DirectPath),
			MediaKey:      up.MediaKey,
			FileEncSHA256: up.FileEncSHA256,
			FileSHA256:    up.FileSHA256,
			FileLength:    proto.Uint64(size),
			Mimetype:      proto.String(mime),
			Caption:       proto.String(caption),
		}}
	case "document":
		if filename == "" {
			filename = "arquivo"
		}
		return &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
			URL:           proto.String(up.URL),
			DirectPath:    proto.String(up.DirectPath),
			MediaKey:      up.MediaKey,
			FileEncSHA256: up.FileEncSHA256,
			FileSHA256:    up.FileSHA256,
			FileLength:    proto.Uint64(size),
			Mimetype:      proto.String(mime),
			FileName:      proto.String(filename),
			Caption:       proto.String(caption),
		}}
	default:
		return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
			URL:           proto.String(up.URL),
			DirectPath:    proto.String(up.DirectPath),
			MediaKey:      up.MediaKey,
			FileEncSHA256: up.FileEncSHA256,
			FileSHA256:    up.FileSHA256,
			FileLength:    proto.Uint64(size),
			Mimetype:      proto.String(mime),
			Caption:       proto.String(caption),
		}}
	}
}

func guessMimeForKind(kind string) string {
	switch strings.ToLower(kind) {
	case "audio":
		return "audio/ogg; codecs=opus"
	case "video":
		return "video/mp4"
	case "document":
		return "application/octet-stream"
	default:
		return "image/jpeg"
	}
}

func parseRecipientJID(to string) (types.JID, error) {
	to = strings.TrimSpace(to)
	if strings.Contains(to, "@") {
		return types.ParseJID(to)
	}
	// Strip +, spaces, hyphens.
	clean := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, to)
	if clean == "" {
		return types.JID{}, errors.New("invalid recipient")
	}
	return types.NewJID(clean, types.DefaultUserServer), nil
}

// ---- audio helpers -----------------------------------------------------

func pcm16BytesToFloat32(b []byte) []float32 {
	n := len(b) / 2
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		s := int16(binary.LittleEndian.Uint16(b[i*2:]))
		out[i] = float32(s) / 32768.0
	}
	return out
}

func encodeWavMonoPCM16(pcm []float32, rate int) []byte {
	dataLen := len(pcm) * 2
	buf := bytes.NewBuffer(make([]byte, 0, 44+dataLen))
	// RIFF header
	buf.WriteString("RIFF")
	_ = binary.Write(buf, binary.LittleEndian, uint32(36+dataLen))
	buf.WriteString("WAVE")
	// fmt chunk
	buf.WriteString("fmt ")
	_ = binary.Write(buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(buf, binary.LittleEndian, uint16(1))      // PCM
	_ = binary.Write(buf, binary.LittleEndian, uint16(1))      // channels
	_ = binary.Write(buf, binary.LittleEndian, uint32(rate))   // sample rate
	_ = binary.Write(buf, binary.LittleEndian, uint32(rate*2)) // byte rate
	_ = binary.Write(buf, binary.LittleEndian, uint16(2))      // block align
	_ = binary.Write(buf, binary.LittleEndian, uint16(16))     // bits/sample
	// data chunk
	buf.WriteString("data")
	_ = binary.Write(buf, binary.LittleEndian, uint32(dataLen))
	for _, s := range pcm {
		if s > 1 {
			s = 1
		} else if s < -1 {
			s = -1
		}
		_ = binary.Write(buf, binary.LittleEndian, int16(s*32767))
	}
	return buf.Bytes()
}
