package call

import (
	"time"
	"wacalls/internal/voip/core"
	"wacalls/internal/voip/media"
	"wacalls/internal/voip/transport"
)

func (m *CallManager) initCodec() {
	if m.codec != nil {
		return
	}
	codec, err := media.NewMLowCodec(media.DefaultCodecOptions)
	if err != nil {
		m.log.Warn("MLow codec unavailable — call will run signaling-only (no audio)", "err", err)
		return
	}
	m.codec = codec
}

func (m *CallManager) FeedCapturedPCM(data []float32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.codec == nil || len(data) == 0 {
		return
	}
	if m.captureRing == nil {
		// 120s of headroom at the codec's sample rate. Ring memory is
		// allocated once and reused for the lifetime of the call.
		m.captureRing = newRingFloat32(m.codec.SampleRate() * 120)
	}
	// Ring.Write drops oldest samples on overflow (same semantics as the
	// previous slice-truncation guard) and never reallocates — eliminates
	// the per-feed copy + GC pressure that previously caused the send loop
	// to stutter under load.
	m.captureRing.Write(data)
}

func (m *CallManager) sendOpusFrameLocked(opus []byte) {
	if m.rtpSession == nil || m.srtpSession == nil {
		return
	}
	marker := !m.firstPacketSent
	pkt := m.rtpSession.CreatePacketWithDuration(opus, m.codec.FrameSize(), marker)
	if m.debeEnabled {
		pkt.Header.Extension = true
		pkt.Header.ExtensionProfile = 0xbede
		pkt.Header.ExtensionData = nil
	}
	m.firstPacketSent = true

	srtp, err := m.srtpSession.Protect(pkt)
	if err != nil {
		m.log.Debug("srtp protect error", "err", err)
		return
	}
	m.relay.Broadcast(srtp)
}

func (m *CallManager) startMediaSendLoopLocked() {
	if m.sendLoopStop != nil || m.codec == nil {
		return
	}
	stop := make(chan struct{})
	m.sendLoopStop = stop
	frameSize := m.codec.FrameSize()
	sampleRate := m.codec.SampleRate()
	frameDur := time.Duration(frameSize) * time.Second / time.Duration(sampleRate)
	go func() {
		// Absolute scheduling: anchor each frame on `start` instead of
		// "sleep frameDur between sends". time.NewTicker accumulates jitter
		// under lock contention and Go scheduler hiccups, which on a busy
		// VPS shows up as audible chop. Anchoring on `start` keeps cadence
		// rock-steady and lets the loop catch up by sending multiple frames
		// in one wake when it falls behind.
		start := time.Now()
		var idx int64
		timer := time.NewTimer(0)
		defer timer.Stop()
		for {
			next := start.Add(time.Duration(idx) * frameDur)
			wait := time.Until(next)
			if wait > 0 {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(wait)
				select {
				case <-stop:
					return
				case <-timer.C:
				}
			} else {
				// Behind schedule — yield briefly then continue without
				// sleeping so we catch up frame-by-frame.
				select {
				case <-stop:
					return
				default:
				}
			}
			idx++
			m.mu.Lock()
			if m.codec == nil || m.rtpSession == nil || m.srtpSession == nil || !m.relay.HasConnection() {
				m.mu.Unlock()
				// Reset clock while the relay is not ready so we don't
				// "burst" a backlog of frames the moment it connects.
				start = time.Now()
				idx = 0
				continue
			}
			// Reusable scratch frame — allocated once per call lifetime.
			// Previously a fresh `make([]float32, frameSize)` was issued
			// every 20ms (~50/s) per active call; on a busy VPS with N
			// concurrent calls that produced N*50 short-lived heap objects
			// per second and triggered frequent GC, starving the send loop.
			if m.frameScratch == nil || len(m.frameScratch) != frameSize {
				m.frameScratch = make([]float32, frameSize)
			}
			frame := m.frameScratch
			n := 0
			if m.captureRing != nil {
				n = m.captureRing.ReadInto(frame)
			}
			if n < frameSize {
				// Zero the unread tail so we encode silence cleanly without
				// allocating a separate silence buffer per frame.
				tail := frame[n:]
				for i := range tail {
					tail[i] = 0
				}
			}
			if opus, err := m.codec.Encode(frame); err == nil {
				m.sendOpusFrameLocked(opus)
			}
			m.mu.Unlock()
		}
	}()
}

func (m *CallManager) onRelayData(data []byte) {
	if transport.IsStunPacket(data) {
		return
	}
	if !transport.IsRtpPacket(data) {
		return
	}
	if len(data) < 12 {
		return
	}
	switch data[1] & 0x7f {
	case core.PayloadTypeWhatsAppOpus:
		m.handleAudioRelayData(data)
	case core.PayloadTypeWhatsAppH264:
		m.handleVideoRelayData(data)
	}
}

func (m *CallManager) handleAudioRelayData(data []byte) {
	m.mu.Lock()
	if m.srtpSession == nil || m.codec == nil {
		m.mu.Unlock()
		return
	}
	ssrc := readRtpSsrc(data)
	if ssrc == m.selfSsrc {
		m.mu.Unlock()
		return
	}
	if !m.actualPeerSet {
		m.actualPeerSet = true
		if !containsSsrc(m.peerSsrcs, ssrc) {
			m.peerSsrcs = []uint32{ssrc}
			m.relay.SetSubscriptionSsrc(ssrc)
			go m.relay.ResendSubscriptions()
		}
	}
	srtp := m.srtpSession
	codec := m.codec
	m.mu.Unlock()

	pkt, err := srtp.Unprotect(data)
	if err != nil {
		m.log.Debug("srtp unprotect error", "err", err)
		return
	}
	if len(pkt.Payload) == 0 {
		return
	}
	pcm, err := codec.Decode(pkt.Payload)
	if err != nil || len(pcm) == 0 {
		return
	}
	if m.OnPeerAudio != nil {
		m.OnPeerAudio(m.alignPeerAudio(pkt.Header.Timestamp, pcm))
	}
}

func (m *CallManager) alignPeerAudio(ts uint32, pcm []float32) []float32 {
	const maxGapSamples = 8000
	m.mu.Lock()
	defer m.mu.Unlock()
	origLen := uint64(len(pcm))
	if !m.audioTimelineSet {
		m.audioTimelineSet = true
		m.audioBaseTs = ts
		m.audioPlayedSamples = origLen
		return pcm
	}
	target := uint64(ts - m.audioBaseTs)
	gap := int64(target) - int64(m.audioPlayedSamples)
	if gap < 0 || gap > maxGapSamples {
		m.audioBaseTs = ts
		m.audioPlayedSamples = origLen
		return pcm
	}
	if gap > 0 {
		padded := make([]float32, int(gap)+int(origLen))
		copy(padded[int(gap):], pcm)
		pcm = padded
	}
	m.audioPlayedSamples = target + origLen
	return pcm
}
