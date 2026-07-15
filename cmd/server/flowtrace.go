package main

import (
	"crypto/rand"
	"encoding/hex"
	"sort"
	"sync"
	"time"
)

// FlowTraceStep is a single correlated event in the lifecycle of an inbound
// URA attempt for one call. Steps are ordered by `At` (UnixMilli) and share
// a stable `TraceID` so backend logs, broker events, and the frontend toast
// can all be cross-referenced when debugging "por que a URA não disparou?".
type FlowTraceStep struct {
	TraceID   string         `json:"traceId"`
	CallID    string         `json:"callId"`
	SessionID string         `json:"sessionId"`
	OwnerID   string         `json:"ownerId,omitempty"`
	At        int64          `json:"at"`
	Level     string         `json:"level"` // info | warn | error
	Code      string         `json:"code"`  // accept | start | skip | tts | node | done
	Message   string         `json:"message,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

// flowTracer keeps a bounded ring of steps per callID so the frontend can
// hit `GET /api/flows/trace?callId=...` and see the exact decision path the
// backend took. Memory-only by design: traces are debugging aid, not audit.
type flowTracer struct {
	mu       sync.RWMutex
	byCall   map[string][]FlowTraceStep
	traceIDs map[string]string // callID -> traceId
	order    []string          // insertion order of callIDs, oldest first
	maxCalls int
	maxSteps int
}

func newFlowTracer() *flowTracer {
	return &flowTracer{
		byCall:   map[string][]FlowTraceStep{},
		traceIDs: map[string]string{},
		maxCalls: 256,
		maxSteps: 64,
	}
}

// TraceIDFor returns (creating if needed) the stable trace id for a callID.
// The id is 8 hex chars — long enough to be unique within a session, short
// enough to fit in a toast and a log prefix.
func (t *flowTracer) TraceIDFor(callID string) string {
	if t == nil || callID == "" {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if id, ok := t.traceIDs[callID]; ok {
		return id
	}
	var b [4]byte
	_, _ = rand.Read(b[:])
	id := hex.EncodeToString(b[:])
	t.traceIDs[callID] = id
	return id
}

func (t *flowTracer) Record(step FlowTraceStep) FlowTraceStep {
	if t == nil || step.CallID == "" {
		return step
	}
	if step.At == 0 {
		step.At = time.Now().UnixMilli()
	}
	if step.TraceID == "" {
		step.TraceID = t.TraceIDFor(step.CallID)
	} else {
		t.mu.Lock()
		if _, ok := t.traceIDs[step.CallID]; !ok {
			t.traceIDs[step.CallID] = step.TraceID
		}
		t.mu.Unlock()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	cur, existed := t.byCall[step.CallID]
	cur = append(cur, step)
	if len(cur) > t.maxSteps {
		cur = cur[len(cur)-t.maxSteps:]
	}
	t.byCall[step.CallID] = cur
	if !existed {
		t.order = append(t.order, step.CallID)
		if len(t.order) > t.maxCalls {
			drop := t.order[0]
			t.order = t.order[1:]
			delete(t.byCall, drop)
			delete(t.traceIDs, drop)
		}
	}
	return step
}

func (t *flowTracer) Get(callID string) []FlowTraceStep {
	if t == nil || callID == "" {
		return nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	cur := t.byCall[callID]
	out := make([]FlowTraceStep, len(cur))
	copy(out, cur)
	return out
}

// ListBySession returns the most recent traces for a session, newest call
// first. Limit caps the number of distinct calls returned.
func (t *flowTracer) ListBySession(sessionID string, limit int) [][]FlowTraceStep {
	if t == nil {
		return nil
	}
	if limit <= 0 {
		limit = 20
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := [][]FlowTraceStep{}
	for i := len(t.order) - 1; i >= 0 && len(out) < limit; i-- {
		steps := t.byCall[t.order[i]]
		if len(steps) == 0 {
			continue
		}
		if sessionID != "" && steps[0].SessionID != sessionID {
			continue
		}
		copyOf := make([]FlowTraceStep, len(steps))
		copy(copyOf, steps)
		out = append(out, copyOf)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i][0].At > out[j][0].At
	})
	return out
}
