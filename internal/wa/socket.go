package wa

import (
	"context"
	"strings"
	"sync"
	"time"

	"wacalls/internal/voip/core"
	"wacalls/internal/voip/signaling"

	"go.mau.fi/whatsmeow"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
)

type Socket struct {
	cli *whatsmeow.Client

	lidCacheMu sync.RWMutex
	lidCache   map[string]lidCacheEntry
}

type lidCacheEntry struct {
	lid       types.JID
	expiresAt time.Time
}

const lidCacheTTL = time.Hour

func NewSocket(cli *whatsmeow.Client) *Socket {
	return &Socket{cli: cli, lidCache: make(map[string]lidCacheEntry)}
}

func (s *Socket) lidFromCache(pn types.JID) (types.JID, bool) {
	s.lidCacheMu.RLock()
	e, ok := s.lidCache[pn.String()]
	s.lidCacheMu.RUnlock()
	if !ok || time.Now().After(e.expiresAt) {
		return types.JID{}, false
	}
	return e.lid, true
}

func (s *Socket) cacheLID(pn, lid types.JID) {
	if lid.IsEmpty() {
		return
	}
	s.lidCacheMu.Lock()
	s.lidCache[pn.String()] = lidCacheEntry{lid: lid, expiresAt: time.Now().Add(lidCacheTTL)}
	s.lidCacheMu.Unlock()
}

func digitsOnly(v string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(v) {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func normalizePN(jid types.JID) types.JID {
	user := digitsOnly(jid.User)
	if user == "" {
		user = jid.User
	}
	return types.NewJID(user, types.DefaultUserServer)
}

func phoneLookupVariants(digits string) []string {
	digits = digitsOnly(digits)
	seen := map[string]bool{}
	out := []string{}
	add := func(d string) {
		d = digitsOnly(d)
		if d == "" || seen[d] {
			return
		}
		seen[d] = true
		out = append(out, "+"+d)
	}
	add(digits)
	// Brazil commonly differs between WhatsApp's canonical number and the
	// dialed mobile number by the 9th digit after the DDD. Try both formats so
	// manual dialer calls can still resolve the recipient LID and ring.
	if !strings.HasPrefix(digits, "55") && (len(digits) == 10 || len(digits) == 11) {
		add("55" + digits)
	}
	if strings.HasPrefix(digits, "55") && len(digits) >= 12 {
		rest := digits[2:]
		if len(rest) == 11 && rest[2] == '9' {
			add("55" + rest[:2] + rest[3:])
		} else if len(rest) == 10 {
			add("55" + rest[:2] + "9" + rest[2:])
		}
	}
	return out
}

var _ core.VoipSocket = (*Socket)(nil)

func (s *Socket) di() *whatsmeow.DangerousInternalClient { return s.cli.DangerousInternals() }

func (s *Socket) OwnPN() types.JID { return s.di().GetOwnID() }

func (s *Socket) OwnLID() types.JID { return s.di().GetOwnLID() }

func (s *Socket) AccountDeviceIdentityNode() (waBinary.Node, bool) {
	if s.cli.Store == nil || s.cli.Store.Account == nil {
		return waBinary.Node{}, false
	}
	return s.di().MakeDeviceIdentityNode(), true
}

func (s *Socket) SendNode(ctx context.Context, node waBinary.Node) error {
	return s.di().SendNode(ctx, node)
}

func (s *Socket) Query(ctx context.Context, node waBinary.Node) (*waBinary.Node, error) {
	id, _ := node.Attrs["id"].(string)
	if id == "" {
		return nil, s.di().SendNode(ctx, node)
	}
	di := s.di()
	ch := di.WaitResponse(id)
	if err := di.SendNode(ctx, node); err != nil {
		di.CancelResponse(id, ch)
		return nil, err
	}
	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(15 * time.Second):
		di.CancelResponse(id, ch)
		return nil, nil
	case <-ctx.Done():
		di.CancelResponse(id, ch)
		return nil, ctx.Err()
	}
}

func (s *Socket) GetUSyncDevices(ctx context.Context, jids []types.JID) ([]types.JID, error) {
	return s.cli.GetUserDevices(ctx, jids)
}

func (s *Socket) AssertSessions(ctx context.Context, jids []types.JID, force bool) error {
	return nil
}

func (s *Socket) CreateParticipantNodes(ctx context.Context, devices []types.JID, callKey []byte, encAttrs waBinary.Attrs) ([]waBinary.Node, bool, error) {
	plaintext, err := signaling.EncodeCallKeyMessage(callKey)
	if err != nil {
		return nil, false, err
	}
	id := s.cli.GenerateMessageID()
	return s.di().EncryptMessageForDevices(ctx, devices, id, plaintext, plaintext, encAttrs)
}

func (s *Socket) DecryptCallKey(ctx context.Context, from types.JID, encChild *waBinary.Node) ([]byte, error) {
	typ, _ := encChild.Attrs["type"].(string)
	isPreKey := typ == "pkmsg"
	plaintext, _, err := s.di().DecryptDM(ctx, encChild, from, isPreKey, time.Now())
	if err != nil {
		return nil, err
	}
	return signaling.DecodeCallKeyPlaintext(plaintext)
}

func (s *Socket) GetTCToken(ctx context.Context, jid types.JID) ([]byte, error) {
	if s.cli.Store == nil || s.cli.Store.PrivacyTokens == nil {
		return nil, nil
	}
	for _, cand := range []types.JID{s.ResolveLIDForPN(ctx, jid).ToNonAD(), jid.ToNonAD()} {
		if cand.IsEmpty() {
			continue
		}
		tok, err := s.cli.Store.PrivacyTokens.GetPrivacyToken(ctx, cand)
		if err != nil {
			return nil, err
		}
		if tok != nil && len(tok.Token) > 0 {
			return tok.Token, nil
		}
	}
	return nil, nil
}

func (s *Socket) ResolveLIDForPN(ctx context.Context, pn types.JID) types.JID {
	// Already a LID (hidden user server) — nothing to resolve.
	if pn.Server == types.HiddenUserServer {
		return pn
	}
	if s.cli == nil || s.cli.Store == nil {
		return pn
	}
	if pn.Server != types.DefaultUserServer && pn.Server != types.LegacyUserServer {
		return pn
	}
	pn = normalizePN(pn)

	// In-memory cache hit (per-session, TTL 1h).
	if lid, ok := s.lidFromCache(pn); ok {
		return lid
	}

	// Fast path: local cache hit on the LID map.
	if s.cli.Store.LIDs != nil {
		if lid, err := s.cli.Store.LIDs.GetLIDForPN(ctx, pn); err == nil && !lid.IsEmpty() {
			s.cacheLID(pn, lid)
			return lid
		}
	}

	// If the operator typed a phone number manually, first ask WhatsApp for the
	// canonical registered PN (including Brazil with/without the 9th digit) and
	// then resolve that PN to LID. This is the path that makes dialer-originated
	// calls ring instead of being dropped silently by WhatsApp.
	if lid, ok := s.resolveLIDViaPhoneLookup(ctx, pn); ok {
		return lid
	}

	if lid, ok := s.resolveLIDViaUserInfo(ctx, pn, "GetUserInfo"); ok {
		return lid
	}

	if lid, ok := s.resolveLIDViaDevices(ctx, pn); ok {
		return lid
	}

	s.cli.Log.Warnf("ResolveLIDForPN: no LID found for %s; outgoing call may not ring", pn)
	return pn
}

func (s *Socket) resolveLIDViaDevices(ctx context.Context, pn types.JID) (types.JID, bool) {
	// Fallback: GetUserDevices triggers a USync that also populates LID mappings
	// for some contacts that don't respond to the "full" sidelist with a LID node.
	devicesCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := s.cli.GetUserDevices(devicesCtx, []types.JID{pn}); err != nil {
		s.cli.Log.Warnf("ResolveLIDForPN: GetUserDevices(%s) failed: %v", pn, err)
	}

	// Re-check the store in case the devices query populated the PN -> LID map.
	if s.cli.Store.LIDs != nil {
		if lid, err := s.cli.Store.LIDs.GetLIDForPN(ctx, pn); err == nil && !lid.IsEmpty() {
			s.cacheLID(pn, lid)
			s.cli.Log.Infof("ResolveLIDForPN: %s -> %s (via store after devices USync)", pn, lid)
			return lid, true
		}
	}
	return types.JID{}, false
}

func (s *Socket) resolveLIDViaUserInfo(ctx context.Context, pn types.JID, source string) (types.JID, bool) {
	// Cache miss: trigger a USync query that explicitly asks for the <lid/>
	// field. GetUserInfo issues a "full/background" usync with
	// {devices, lid, status, picture} and calls Store.LIDs.PutManyLIDMappings
	// with the result.
	syncCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	info, err := s.cli.GetUserInfo(syncCtx, []types.JID{pn})
	if err != nil {
		s.cli.Log.Warnf("ResolveLIDForPN: %s(%s) failed: %v", source, pn, err)
		return types.JID{}, false
	}
	for key, u := range info {
		if u.LID.IsEmpty() {
			continue
		}
		s.cacheLID(pn, u.LID)
		if key.Server == types.DefaultUserServer || key.Server == types.LegacyUserServer {
			s.cacheLID(normalizePN(key), u.LID)
		}
		s.cli.Log.Infof("ResolveLIDForPN: %s -> %s (via %s)", pn, u.LID, source)
		return u.LID, true
	}
	return types.JID{}, false
}

func (s *Socket) resolveLIDViaPhoneLookup(ctx context.Context, pn types.JID) (types.JID, bool) {
	phones := phoneLookupVariants(pn.User)
	if len(phones) == 0 {
		return types.JID{}, false
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	rows, err := s.cli.IsOnWhatsApp(lookupCtx, phones)
	if err != nil {
		s.cli.Log.Warnf("ResolveLIDForPN: IsOnWhatsApp(%s) failed: %v", pn, err)
		return types.JID{}, false
	}
	for _, row := range rows {
		if !row.IsIn || row.JID.IsEmpty() {
			continue
		}
		if row.JID.Server == types.HiddenUserServer {
			s.cacheLID(pn, row.JID)
			return row.JID, true
		}
		candidate := normalizePN(row.JID)
		if candidate.User == "" {
			continue
		}
		if candidate.User != pn.User {
			s.cli.Log.Infof("ResolveLIDForPN: canonical phone %s -> %s", pn, candidate)
		}
		if lid, ok := s.lidFromCache(candidate); ok {
			s.cacheLID(pn, lid)
			return lid, true
		}
		if s.cli.Store.LIDs != nil {
			if lid, err := s.cli.Store.LIDs.GetLIDForPN(ctx, candidate); err == nil && !lid.IsEmpty() {
				s.cacheLID(candidate, lid)
				s.cacheLID(pn, lid)
				return lid, true
			}
		}
		if lid, ok := s.resolveLIDViaUserInfo(ctx, candidate, "IsOnWhatsApp+GetUserInfo"); ok {
			s.cacheLID(pn, lid)
			return lid, true
		}
		if lid, ok := s.resolveLIDViaDevices(ctx, candidate); ok {
			s.cacheLID(pn, lid)
			return lid, true
		}
	}
	return types.JID{}, false
}
