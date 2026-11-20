package assistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/feishubot"
)

const (
	briefDeliveriesKey  = "assistant.brief_deliveries"
	larkBriefDigestTool = "larkcontext_digest"
	briefDeliveryPrompt = "\n\n本次飞书定向简报由 Persona 投递。"
	briefDeliveryPoll   = 10 * time.Second
	// Keep every retry within the scheduled occurrence and Feishu's one-hour UUID window.
	briefDeliveryWindow = 30 * time.Minute
)

var beijingBriefZone = time.FixedZone("Asia/Shanghai", 8*60*60)

type larkBriefDigestReader interface {
	ReadLarkBriefDigest(context.Context, json.RawMessage) (string, error)
}

func (s *Service) targetsLarkBrief(t Task) bool {
	if !t.Enabled || !t.Facts || !t.calendar() || t.Space == SpaceHealth || t.pushesOnlyAsked() ||
		(t.ID != "morning" && t.ID != "evening") || s.ext == nil {
		return false
	}
	_, readOnly := s.ext.ToolAccess(larkBriefDigestTool)
	// A saved Lark briefing still owns its route while the connector catalog is unavailable.
	return readOnly || strings.Contains(t.Prompt, larkBriefDigestTool)
}

type briefDelivery struct {
	Routine     string    `json:"routine"`
	Due         time.Time `json:"due"`
	Slot        string    `json:"slot"`
	RunID       string    `json:"run_id"`
	Machine     string    `json:"machine_id,omitempty"`
	Session     string    `json:"session_id,omitempty"`
	Owner       string    `json:"owner,omitempty"`
	PairedAt    time.Time `json:"paired_at,omitzero"`
	UUID        string    `json:"uuid,omitempty"`
	Card        string    `json:"card,omitempty"` // exact Feishu payload, saved before the first attempt
	Status      string    `json:"status"`
	Error       string    `json:"error,omitempty"`
	MessageID   string    `json:"message_id,omitempty"`
	ConfirmedAt time.Time `json:"confirmed_at,omitzero"`
}

type briefDeliveryBook struct {
	mu   sync.Mutex
	rows map[string]briefDelivery
}

func (b *briefDeliveryBook) loadLocked(ctx context.Context, st Store) error {
	if b.rows != nil {
		return nil
	}
	rows := make(map[string]briefDelivery)
	if _, err := st.Setting(ctx, briefDeliveriesKey, &rows); err != nil {
		return err
	}
	if rows == nil {
		rows = make(map[string]briefDelivery)
	}
	b.rows = rows
	return nil
}

func (b *briefDeliveryBook) saveLocked(ctx context.Context, st Store, key string, row briefDelivery) error {
	b.rows[key] = row
	if err := st.PutSetting(ctx, briefDeliveriesKey, b.rows); err != nil {
		b.rows = nil // the write may have committed before the reply was lost
		return err
	}
	return nil
}

func (d briefDelivery) deliveryWindow() (start, deadline time.Time) {
	local := d.Due.In(beijingBriefZone)
	y, m, day := local.Date()
	morning := time.Date(y, m, day, 9, 0, 0, 0, beijingBriefZone)
	evening := time.Date(y, m, day, 20, 30, 0, 0, beijingBriefZone)
	if d.Routine == "morning" {
		return morning, earlierBriefTime(d.Due.Add(briefDeliveryWindow), evening)
	}
	return evening, earlierBriefTime(d.Due.Add(briefDeliveryWindow), time.Date(y, m, day+1, 0, 0, 0, 0, beijingBriefZone))
}

func earlierBriefTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (d *briefDelivery) finish(now time.Time, status, reason string) {
	d.ConfirmedAt, d.Status, d.Error, d.Card = now.UTC(), status, reason, ""
}

func (s *Service) enqueueBriefDelivery(t Task, run ScheduleRun) {
	if !t.targetBrief || run.ID == "" || run.Manual || run.Due.IsZero() {
		return
	}
	st := s.store()
	if st == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	key := briefFallbackKey(run)
	row := briefDelivery{Routine: run.RefID, Due: run.Due.UTC(), RunID: run.ID, Machine: run.MachineID, Session: run.SessionID,
		Slot: run.Due.In(beijingBriefZone).Format(time.DateOnly) + ":" + run.RefID, Status: "pending"}
	if row.Session == "" {
		row.Machine, row.Session = t.MachineID, t.SessionID
	}
	b := &s.briefDeliveries
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.loadLocked(ctx, st); err != nil {
		s.logf("briefing delivery %s: read receipts: %v", key, err)
		return
	}
	if _, exists := b.rows[key]; exists {
		return
	}
	owner, err := s.feishuStatus(ctx)
	now := time.Now()
	if err != nil {
		row.finish(now, "failed", "could not verify the Feishu owner binding")
	} else if owner.Owner == "" {
		row.finish(now, "failed", "no bound Feishu owner")
	} else {
		row.Owner, row.PairedAt = owner.Owner, owner.PairedAt
		sum := sha256.Sum256([]byte("lark-brief\x00" + row.Owner + "\x00" + key))
		row.UUID = "brief-" + hex.EncodeToString(sum[:16])
	}
	if err := b.saveLocked(ctx, st, key, row); err != nil {
		s.logf("briefing delivery %s: save occurrence: %v", key, err)
	} else if row.Status == "failed" {
		s.logf("briefing delivery %s run=%s failed: %s", key, row.RunID, row.Error)
	}
}

func (s *Service) briefDeliveryLoop(ctx context.Context) {
	ticker := time.NewTicker(briefDeliveryPoll)
	defer ticker.Stop()
	for {
		if !s.hello(ctx).Assistant.Standby {
			s.briefDeliveryTick(ctx, time.Now())
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) briefDeliveryTick(ctx context.Context, now time.Time) {
	st := s.store()
	if st == nil {
		return
	}
	b := &s.briefDeliveries
	pending := make(map[string]briefDelivery)
	b.mu.Lock()
	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err := b.loadLocked(rctx, st)
	cancel()
	for key, row := range b.rows {
		if row.Status == "pending" {
			pending[key] = row
		}
	}
	b.mu.Unlock()
	if err != nil {
		s.logf("briefing delivery: recover receipts: %v", err)
		return
	}
	for key, row := range pending {
		if ctx.Err() != nil {
			return
		}
		s.advanceBriefDelivery(ctx, st, key, row, now)
	}
}

func (s *Service) advanceBriefDelivery(ctx context.Context, st Store, key string, row briefDelivery, now time.Time) {
	if s.maintenanceGate != nil {
		done, err := s.maintenanceGate.AdmitPayload("brief-delivery", row.UUID, nil)
		if err != nil {
			return
		}
		defer done()
	}
	save := func(updated briefDelivery) bool {
		wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		b := &s.briefDeliveries
		b.mu.Lock()
		defer b.mu.Unlock()
		err := b.loadLocked(wctx, st)
		if err == nil {
			err = b.saveLocked(wctx, st, key, updated)
		}
		if err != nil {
			s.logf("briefing delivery %s: save receipt: %v", key, err)
			return false
		}
		return true
	}
	start, deadline := row.deliveryWindow()
	if now.Before(row.Due) {
		return
	}
	next := row
	switch {
	case !deadline.After(start) || !row.Due.Before(deadline):
		next.finish(now, "failed", "planned due is outside the requested Beijing slot")
	case !now.Before(deadline):
		reason := row.Error
		if reason == "" {
			reason = "no confirmed delivery"
		}
		next.finish(now, "failed", "delivery window expired: "+reason)
	case now.Before(start):
		return
	default:
		attempt, cancel := context.WithTimeout(ctx, min(deadline.Sub(now), briefDeliveryPoll))
		next = s.tryBriefDelivery(attempt, row, deadline, save)
		cancel()
	}
	if next != row && save(next) && next.Status != "pending" {
		if next.Status == "sent" && next.Session != "" {
			s.rememberBotThread(next.MessageID, botTarget{Kind: targetPersona, MachineID: next.Machine, SessionID: next.Session}, next.Slot)
		}
		s.logf("briefing delivery %s slot=%s run=%s status=%s message=%s reason=%s", key, next.Slot, next.RunID, next.Status, next.MessageID, next.Error)
	}
}

func (s *Service) tryBriefDelivery(ctx context.Context, row briefDelivery, deadline time.Time, save func(briefDelivery) bool) briefDelivery {
	replaying := row.Card != ""
	fail := func(reason string) briefDelivery {
		if replaying {
			reason = "previous send unconfirmed: " + reason
		}
		row.finish(time.Now(), "failed", reason)
		return row
	}
	st, err := s.feishuStatus(ctx)
	if err != nil || (st.Enabled && !st.Ready && st.Owner == row.Owner && st.PairedAt.Equal(row.PairedAt)) {
		row.Error = "Feishu bot is temporarily unavailable"
		return row
	}
	if st.Owner != row.Owner || !st.PairedAt.Equal(row.PairedAt) {
		return fail("Feishu owner binding changed")
	}
	if !st.Enabled {
		return fail("Feishu bot is disabled")
	}
	sc, err := s.lockedSchedule(ctx)
	if err != nil {
		row.Error = "briefing routine could not be read"
		return row
	}
	i := sc.indexLocked(row.Routine)
	active := i >= 0 && sc.tasks[i].Enabled && sc.tasks[i].Facts && sc.tasks[i].calendar() &&
		sc.tasks[i].Space != SpaceHealth && !sc.tasks[i].pushesOnlyAsked()
	sc.mu.Unlock()
	if !active {
		return fail("briefing routine is unavailable or disabled")
	}
	if row.Session != "" {
		sess, ok := s.session(ctx, row.Machine, row.Session)
		if !ok {
			row.Error = "briefing conversation could not be checked"
			return row
		}
		if spaceOf(sess) == SpaceHealth || s.justMeNow(row.Machine, row.Session) {
			return fail("briefing conversation is private")
		}
	}
	if row.Card == "" {
		card, why := s.readBriefDeliveryCard(ctx, row)
		if why != "" {
			row.Error = why
			return row
		}
		prepared := row
		prepared.Card, prepared.Error = card, "Feishu send unconfirmed"
		if !save(prepared) {
			return row
		}
		row = prepared
	}
	var res struct {
		MessageID string `json:"message_id"`
	}
	err = s.appCall(ctx, AppFeishu, assistantwire.FeishuBriefPost, assistantwire.FeishuBriefPostParams{
		Owner: row.Owner, PairedAt: row.PairedAt, ExpiresAt: deadline, UUID: row.UUID, Card: row.Card,
	}, &res)
	switch {
	case err == nil && res.MessageID != "":
		row.MessageID = res.MessageID
		row.finish(time.Now(), "sent", "")
	case err == nil:
		row.Error = "Feishu returned no message ID"
	case feishubot.Transient(err):
		row.Error = "Feishu send unconfirmed: " + clip(oneLine(err.Error()), 200)
	default:
		return fail("Feishu send rejected: " + clip(oneLine(err.Error()), 200))
	}
	return row
}

func (s *Service) readBriefDeliveryCard(ctx context.Context, row briefDelivery) (card, why string) {
	reader, ok := s.ext.(larkBriefDigestReader)
	if !ok {
		return "", "Lark Context digest tool is unavailable"
	}
	day := row.Due.In(beijingBriefZone).Format(time.DateOnly)
	args, _ := json.Marshal(map[string]any{"day": day, "slot": row.Routine, "include_fyi_items": true})
	raw, err := reader.ReadLarkBriefDigest(ctx, args)
	if err != nil {
		return "", "Lark Context digest read failed: " + clip(oneLine(err.Error()), 200)
	}
	var digest struct {
		Day    string `json:"day"`
		Slot   string `json:"requested_slot"`
		Status string `json:"digest_status"`
		Issue  struct {
			Code string `json:"code"`
		} `json:"digest_issue"`
		Latest struct {
			ID      string `json:"id"`
			Title   string `json:"title"`
			Content string `json:"content"`
		} `json:"latest"`
	}
	if err := json.Unmarshal([]byte(raw), &digest); err != nil {
		return "", "Lark Context digest read returned invalid JSON"
	}
	if digest.Day != day || digest.Slot != row.Slot {
		return "", "Lark Context response did not match the requested slot"
	}
	if digest.Status != "ready" {
		return "", fmt.Sprintf("Lark Context digest %s (%s)", clip(oneLine(digest.Status), 40), clip(oneLine(digest.Issue.Code), 100))
	}
	if digest.Latest.ID != row.Slot || strings.TrimSpace(digest.Latest.Content) == "" {
		return "", "Lark Context has no usable digest for the requested slot"
	}
	title := clip(oneLine(digest.Latest.Title), 80)
	content := strings.TrimPrefix(digest.Latest.Content, digest.Latest.Title+"\n\n")
	for limit := len([]rune(content)); ; limit /= 2 {
		card := feishubot.NewCard(title, feishubot.Blue, title).Markdown(feishubot.CardMarkdown(clip(content, limit))).JSON()
		if len(card) <= answerCardMaxBytes {
			return card, ""
		}
	}
}
