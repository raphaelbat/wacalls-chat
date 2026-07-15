package main

import (
	"context"
	"sort"
	"strings"
)

// backfillKanbanCards materializes one card per active chat (atendimento)
// across every session the requesting user can see, placing cards in the
// column that matches the chat's lifecycle status:
//
//	waiting  -> first "open" column   (A FAZER)
//	open     -> second "open" column  (EM ANDAMENTO), fallback to the first
//	closed   -> first "won" column    (CONCLUÍDO)   — skipped if none
//
// Existing cards for (sessionID, chatJID) on the board are left untouched so
// the operator's manual moves are never overwritten. Failures are logged via
// the server logger and never block board rendering.
func (s *server) backfillKanbanCards(ctx context.Context, boardID string, columns []kanbanColumn, userID string, isAdmin bool) {
	if s.kanban == nil || s.messages == nil || s.chatMeta == nil || s.sessions == nil {
		return
	}
	if len(columns) == 0 {
		return
	}

	// Pick destination columns by stage type, preserving position order.
	sorted := append([]kanbanColumn(nil), columns...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Position < sorted[j].Position })
	var waitingCol, progressCol, wonCol string
	openSeen := 0
	for _, c := range sorted {
		switch c.StageType {
		case "open":
			if openSeen == 0 {
				waitingCol = c.ID
			} else if openSeen == 1 {
				progressCol = c.ID
			}
			openSeen++
		case "won":
			if wonCol == "" {
				wonCol = c.ID
			}
		}
	}
	if progressCol == "" {
		progressCol = waitingCol
	}
	if waitingCol == "" {
		// Board has no "open" stage; nothing reasonable to seed.
		return
	}

	for _, info := range s.sessions.infosFor(userID, isAdmin) {
		sid := info.ID
		if sid == "" {
			continue
		}
		summaries, err := s.messages.ListChats(ctx, sid)
		if err != nil {
			continue
		}
		metas, _ := s.chatMeta.ListBySession(ctx, sid)

		for _, sum := range summaries {
			jid := sum.ChatJID
			if jid == "" {
				continue
			}
			// Skip non-conversation JIDs and groups.
			if strings.HasSuffix(jid, "@g.us") ||
				strings.Contains(jid, "@broadcast") ||
				strings.Contains(jid, "@newsletter") ||
				strings.HasSuffix(jid, "status@broadcast") {
				continue
			}

			// Compute the column the current chat status maps to so we can
			// either create a new card or reconcile an existing one.
			meta := metas[jid]
			var targetCol string
			switch meta.Status {
			case ChatStatusClosed:
				if wonCol == "" {
					// No "won" stage on this board; leave whatever card is
					// already there alone and skip creation.
					targetCol = ""
				} else {
					targetCol = wonCol
				}
			case ChatStatusOpen:
				targetCol = progressCol
			case ChatStatusGroup:
				continue
			default:
				targetCol = waitingCol
			}

			// If a card already exists on THIS board, auto-move it whenever
			// it still lives in one of the 3 stages we manage and the chat
			// status now points elsewhere. Manual moves to custom columns
			// (anything outside waiting/progress/won) are preserved.
			existing, _ := s.kanban.CardsByChat(ctx, sid, jid)
			var current *kanbanCard
			for i := range existing {
				if existing[i].BoardID == boardID {
					c := existing[i]
					current = &c
					break
				}
			}
			if current != nil {
				if targetCol != "" && current.ColumnID != targetCol {
					managed := current.ColumnID == waitingCol ||
						current.ColumnID == progressCol ||
						current.ColumnID == wonCol
					if managed {
						if err := s.kanban.MoveCard(ctx, current.ID, targetCol, 0); err != nil && s.log != nil {
							s.log.Warn("kanban backfill: move card failed", "card", current.ID, "err", err)
						}
					}
				}
				continue
			}

			if targetCol == "" {
				continue
			}

			title := strings.TrimSpace(meta.Name)
			if title == "" {
				// Fall back to the JID's local part so the card has *some*
				// label; the frontend already prettifies LIDs and phones.
				if at := strings.IndexByte(jid, '@'); at > 0 {
					title = jid[:at]
				} else {
					title = jid
				}
			}

			if _, err := s.kanban.CreateCard(ctx, cardCreate{
				BoardID:   boardID,
				ColumnID:  targetCol,
				Title:     title,
				SessionID: sid,
				ChatJID:   jid,
			}); err != nil && s.log != nil {
				s.log.Warn("kanban backfill: create card failed", "sid", sid, "jid", jid, "err", err)
			}
		}
	}
}
