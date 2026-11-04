package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/proto"
	wechat "github.com/nwverse/persona/server/internal/wechat/spec"
)

// Before the channels' pipeline the WeChat program kept, in its own sealed state, where the owner's messages went
// (/new), the turns it waited on and what it had asked in text. migrateWeChatState moves those three over to the
// hub's (channels.go, channels_runs.go) once, before the program starts: the new program keeps only its token, cursor,
// context token and the messages it saw, and would drop the rest the first time it saves.

type wechatLegacy struct {
	Target *struct {
		Machine string `json:"machine"`
		Session string `json:"session"`
		Fresh   bool   `json:"fresh"`
	} `json:"target"`
	Runs []struct {
		Machine string    `json:"machine"`
		Session string    `json:"session"`
		Base    int       `json:"base"`
		MsgIDs  []string  `json:"msg_ids"`
		Probe   string    `json:"probe"`
		Started time.Time `json:"started"`
		Seen    bool      `json:"seen"`
		Held    bool      `json:"held"`
	} `json:"runs"`
	Asks []struct {
		ID        string            `json:"id"`
		Machine   string            `json:"machine"`
		Session   string            `json:"session"`
		Kind      string            `json:"kind"`
		Title     string            `json:"title"`
		Questions []proto.Question  `json:"questions"`
		Answers   map[string]string `json:"answers"`
		Step      int               `json:"step"`
		Options   []string          `json:"options"`
		At        time.Time         `json:"at"`
	} `json:"asks"`
}

func (h *Hub) migrateWeChatState(ctx context.Context) {
	if h.store == nil {
		return
	}
	sec := h.secretsFor(wechat.ID)
	data, err := sec.Load(ctx)
	if err != nil || len(data) == 0 {
		return
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil {
		return
	}
	_, t := raw["target"]
	_, r := raw["runs"]
	_, a := raw["asks"]
	if !t && !r && !a {
		return
	}
	var old wechatLegacy
	if err := json.Unmarshal(data, &old); err != nil {
		h.logf("channels: wechat's old state does not read: %v", err)
		return
	}
	c := &h.channels
	c.mu.Lock()
	h.loadChannelsLocked(ctx)
	h.loadChannelStateLocked(ctx)
	if p := h.channelPrefLocked(wechat.ID); p.Target == nil && old.Target != nil {
		switch {
		case old.Target.Fresh:
			p.Target = &channel.Target{Kind: channel.TargetNew}
		case old.Target.Session != "":
			p.Target = &channel.Target{Kind: channel.TargetConversation, MachineID: old.Target.Machine, SessionID: old.Target.Session}
		}
	}
	for i, o := range old.Runs {
		if o.Session == "" {
			continue
		}
		c.st.Runs = append(c.st.Runs, &channel.Run{ID: fmt.Sprintf("%s_migrated_%d", wechat.ID, i), Channel: wechat.ID,
			MachineID: o.Machine, SessionID: o.Session, TargetKind: channel.TargetConversation, Base: o.Base, MsgIDs: o.MsgIDs, Probe: o.Probe,
			Started: o.Started, Seen: o.Seen, Held: o.Held})
	}
	for _, o := range old.Asks {
		c.st.Asks = append(c.st.Asks, &channel.Ask{ID: o.ID, Channel: wechat.ID, MachineID: o.Machine, SessionID: o.Session, Kind: o.Kind,
			Title: o.Title, Questions: o.Questions, Answers: o.Answers, Step: o.Step, Options: o.Options, At: o.At})
	}
	h.saveChannelsLocked()
	h.saveChannelStateLocked()
	c.mu.Unlock()
	delete(raw, "target")
	delete(raw, "runs")
	delete(raw, "asks")
	out, _ := json.Marshal(raw)
	if err := sec.Save(ctx, out); err != nil {
		h.logf("channels: wechat's state: %v", err)
		return
	}
	h.logf("channels: wechat's target, %d turns and %d questions moved to the hub", len(old.Runs), len(old.Asks))
}
