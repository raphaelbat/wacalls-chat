package main

import (
	"context"
	"math/rand"
	"strings"
	"time"
)

// Distribution strategies configurable per queue.
const (
	DistributionManual     = "manual"
	DistributionRoundRobin = "round-robin"
	DistributionLeastBusy  = "least-busy"
	DistributionRandom     = "random"
)

func normalizeDistribution(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case DistributionRoundRobin, "roundrobin", "rodizio":
		return DistributionRoundRobin
	case DistributionLeastBusy, "leastbusy", "menos-ocupado":
		return DistributionLeastBusy
	case DistributionRandom:
		return DistributionRandom
	default:
		return DistributionManual
	}
}

// autoRouteChat assigns a freshly-arrived conversation to an operator using
// the queue's distribution strategy. It is a no-op for groups, for chats that
// already have an owner, and for queues configured as "manual".
func (s *server) autoRouteChat(ctx context.Context, sessionID, chatJID string) {
	if s.chatMeta == nil || s.queues == nil || isGroupChatJID(chatJID) {
		return
	}
	meta, found, err := s.chatMeta.Get(ctx, sessionID, chatJID)
	if err != nil {
		return
	}
	if found && (strings.TrimSpace(meta.AssignedUserID) != "" || meta.Status == ChatStatusGroup) {
		return
	}
	if found && meta.Status == ChatStatusOpen {
		return
	}
	queueID := ""
	if found {
		queueID = strings.TrimSpace(meta.QueueID)
	}
	if queueID == "" {
		if sess, ok := s.sessions.Get(sessionID); ok {
			sess.mu.Lock()
			queueID = strings.TrimSpace(sess.queueID)
			sess.mu.Unlock()
		}
	}
	if queueID == "" {
		return
	}
	queue, err := s.queues.Get(ctx, queueID)
	if err != nil {
		return
	}
	strategy := normalizeDistribution(queue.Distribution)
	if strategy == DistributionManual {
		return
	}
	// Outside business hours nothing is distributed: the out-of-hours reply
	// already took care of the customer.
	if cfg, ok := s.resolveBusinessHours(ctx, sessionID, queueID); ok {
		if open, _ := cfg.isOpenAt(time.Now()); !open {
			return
		}
	}
	agent := s.pickAgent(ctx, queue, strategy)
	if agent == "" {
		return
	}
	now := time.Now().UnixMilli()
	if err := s.chatMeta.SetAssignment(ctx, sessionID, chatJID, ChatStatusOpen, agent, queueID, now); err != nil {
		s.log.Warn("auto routing failed", "session", sessionID, "chat", chatJID, "err", err)
		return
	}
	_ = s.queues.SetLastAgent(ctx, queueID, agent)
	s.logChatEvent(ctx, sessionID, chatJID, "transferred", "", "", "auto:"+strategy+" queue="+queueID, now)
	if m, ok, _ := s.chatMeta.Get(ctx, sessionID, chatJID); ok {
		s.broker.emitChatMeta(m)
	}
}

// pickAgent returns the operator that should receive the next conversation,
// honouring the queue's maximum simultaneous load.
func (s *server) pickAgent(ctx context.Context, queue queueRow, strategy string) string {
	members, err := s.queues.Members(ctx, queue.ID)
	if err != nil || len(members) == 0 {
		return ""
	}
	type candidate struct {
		id   string
		load int
	}
	pool := make([]candidate, 0, len(members))
	for _, id := range members {
		load, err := s.chatMeta.OpenLoad(ctx, id)
		if err != nil {
			continue
		}
		if queue.MaxLoad > 0 && load >= queue.MaxLoad {
			continue
		}
		pool = append(pool, candidate{id: id, load: load})
	}
	if len(pool) == 0 {
		return ""
	}
	switch strategy {
	case DistributionRandom:
		return pool[rand.Intn(len(pool))].id
	case DistributionLeastBusy:
		best := pool[0]
		for _, c := range pool[1:] {
			if c.load < best.load {
				best = c
			}
		}
		return best.id
	default: // round-robin
		start := 0
		for i, c := range pool {
			if c.id == queue.LastAgentID {
				start = i + 1
				break
			}
		}
		return pool[start%len(pool)].id
	}
}
