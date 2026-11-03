package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/chyroc/jarvis/server/internal/connector"
)

// Muting (docs/inbox.md「静音」, docs/connectors.md「二 · 5」): the user mutes a repository, a chat or a sender from an
// item's menu; the hub keeps the mute (settings "inbox_mutes") and, the same for every source, files the open items it
// covers as done and the ones that come in later as soon as they do. What an item can be muted by (its `mutes`) is a
// connector's tags (InboxThing.Tags) or, for Feishu and mail, read off the item.

// inboxMutesKey is where the mutes are kept.
const inboxMutesKey = "inbox_mutes"

// MuteOption is one thing an item can be muted by: a tag (connector.TagRepo, TagChat, TagSender) and its value,
// with a name for people.
type MuteOption struct {
	Tag   string `json:"tag"`
	Value string `json:"value"`
	Name  string `json:"name"`
}

// InboxMute is a mute the user set: nothing of Source tagged Tag = Value stays in the inbox.
type InboxMute struct {
	ID         string    `json:"id"`
	Source     string    `json:"source"`
	SourceName string    `json:"source_name,omitempty"` // listed only
	Tag        string    `json:"tag"`
	Value      string    `json:"value"`
	Name       string    `json:"name"`
	CreatedAt  time.Time `json:"created_at"`
}

var mutableTags = []string{connector.TagRepo, connector.TagChat, connector.TagSender}

// mutesOf is what it can be muted by.
func mutesOf(it *InboxItem) []MuteOption {
	var out []MuteOption
	add := func(tag, value, name string) {
		if value == "" || slices.ContainsFunc(out, func(m MuteOption) bool { return m.Tag == tag }) {
			return
		}
		if name == "" {
			name = value
		}
		out = append(out, MuteOption{Tag: tag, Value: value, Name: name})
	}
	switch {
	case it.Connector != nil:
		for _, t := range it.Connector.Tags {
			if slices.Contains(mutableTags, t.Key) {
				add(t.Key, t.Value, t.Name)
			}
		}
	case it.Lark != nil && it.Doc == nil && it.Moment == nil:
		add(connector.TagChat, it.Lark.FeedID, it.Title)
		if it.Lark.IsGroup {
			add(connector.TagSender, it.Lark.SenderID, it.Sender)
		}
	case it.Mail != nil:
		add(connector.TagSender, strings.ToLower(it.Mail.From.Email), it.Mail.From.Name)
	}
	return out
}

// mutedBy is the mute that covers it, nil when none does.
func mutedBy(mutes []InboxMute, it *InboxItem) *InboxMute {
	for _, o := range mutesOf(it) {
		for i := range mutes {
			if m := &mutes[i]; m.Source == it.Source && m.Tag == o.Tag && m.Value == o.Value {
				return m
			}
		}
	}
	return nil
}

// applyMutes files the open items the mutes cover as done (read), saying which mute; under b.mu.
func (b *inbox) applyMutes(now time.Time) []*InboxItem {
	if len(b.mutes) == 0 {
		return nil
	}
	var changed []*InboxItem
	for _, it := range b.items {
		if !it.open() {
			continue
		}
		if m := mutedBy(b.mutes, it); m != nil {
			it.done(now, "read")
			it.Reason = "已静音：" + m.Name
			changed = append(changed, it)
		}
	}
	return changed
}

// loadInboxMutes reads the mutes kept, joining any added before (GitHub's moved muted repositories, githubMutes may run
// first: the inbox and the connectors start side by side).
func (h *Hub) loadInboxMutes(ctx context.Context) {
	if h.store == nil {
		return
	}
	h.inbox.mutesMu.Lock()
	defer h.inbox.mutesMu.Unlock()
	var kept []InboxMute
	if _, err := h.store.Setting(ctx, inboxMutesKey, &kept); err != nil {
		h.logf("inbox: mutes: %v", err)
		return
	}
	h.inbox.mu.Lock()
	for _, m := range h.inbox.mutes {
		if !slices.ContainsFunc(kept, func(k InboxMute) bool { return k.Source == m.Source && k.Tag == m.Tag && k.Value == m.Value }) {
			kept = append(kept, m)
		}
	}
	h.inbox.mutes = kept
	h.inbox.mu.Unlock()
}

// saveMutes keeps the mutes as they are now; under mutesMu.
func (h *Hub) saveMutes(ctx context.Context) error {
	if h.store == nil {
		return nil
	}
	h.inbox.mu.Lock()
	mutes := slices.Clone(h.inbox.mutes)
	h.inbox.mu.Unlock()
	return h.store.PutSetting(ctx, inboxMutesKey, mutes)
}

// addMute adds a mute (the same one twice is one) and files what it covers; it returns the mute and how many
// items it filed.
func (h *Hub) addMute(ctx context.Context, m InboxMute) (InboxMute, int, error) {
	h.inbox.mutesMu.Lock()
	defer h.inbox.mutesMu.Unlock()
	h.inbox.mu.Lock()
	i := slices.IndexFunc(h.inbox.mutes, func(x InboxMute) bool { return x.Source == m.Source && x.Tag == m.Tag && x.Value == m.Value })
	if i >= 0 {
		m = h.inbox.mutes[i]
	} else {
		if m.ID == "" {
			m.ID = fmt.Sprintf("m%d", time.Now().UnixNano())
		}
		if m.CreatedAt.IsZero() {
			m.CreatedAt = time.Now().UTC()
		}
		h.inbox.mutes = append(h.inbox.mutes, m)
	}
	changed := h.inbox.applyMutes(time.Now())
	h.inbox.mu.Unlock()
	if err := h.saveMutes(ctx); err != nil {
		return m, 0, err
	}
	h.saveInbox(ctx, changed)
	return m, len(changed), nil
}

// removeMutes takes the mutes drop says yes to away; what they covered stays done.
func (h *Hub) removeMutes(ctx context.Context, drop func(InboxMute) bool) error {
	h.inbox.mutesMu.Lock()
	defer h.inbox.mutesMu.Unlock()
	h.inbox.mu.Lock()
	h.inbox.mutes = slices.DeleteFunc(slices.Clone(h.inbox.mutes), drop)
	h.inbox.mu.Unlock()
	return h.saveMutes(ctx)
}

// ---- GitHub's muted repositories ----
//
// GitHub's settings page (and older apps) keep muted repositories in its options ("muted_repos"). They are the
// mutes of source github, tag repo now: listed in its options from the mutes, set through them into the mutes, and
// the ones kept in its options before are moved over once (githubMutes).

const githubMutedRepos = "muted_repos"

// githubRepoMute is the mute of a GitHub repository.
func githubRepoMute(repo string) InboxMute {
	return InboxMute{Source: "github", Tag: connector.TagRepo, Value: strings.ToLower(repo), Name: repo}
}

func isGitHubRepoMute(m InboxMute) bool { return m.Source == "github" && m.Tag == connector.TagRepo }

// githubMutes moves the repositories GitHub's options still mute into the mutes and empties them there.
func (h *Hub) githubMutes(ctx context.Context) {
	o, ok := connector.As[connector.Options](h.connectors.Get("github"))
	if !ok {
		return
	}
	var opts map[string]json.RawMessage
	var repos []string
	if json.Unmarshal(o.Options(), &opts) != nil || json.Unmarshal(opts[githubMutedRepos], &repos) != nil || len(repos) == 0 {
		return
	}
	for _, r := range repos {
		if _, _, err := h.addMute(ctx, githubRepoMute(r)); err != nil {
			h.logf("inbox: github's muted %s: %v", r, err)
			return
		}
	}
	opts[githubMutedRepos] = json.RawMessage(`[]`)
	raw, _ := json.Marshal(opts)
	if err := h.applyConnectorOptions(ctx, "github", raw); err != nil {
		h.logf("inbox: github's muted repositories moved, its options kept them: %v", err)
	}
}

// withGitHubMutes is GitHub's options as listed: the muted repositories are the mutes.
func (h *Hub) withGitHubMutes(raw json.RawMessage) json.RawMessage {
	var opts map[string]json.RawMessage
	if json.Unmarshal(raw, &opts) != nil {
		return raw
	}
	repos := []string{}
	for _, m := range h.listedMutes() {
		if isGitHubRepoMute(m) {
			repos = append(repos, m.Name)
		}
	}
	opts[githubMutedRepos], _ = json.Marshal(repos)
	out, _ := json.Marshal(opts)
	return out
}

// setGitHubMutes takes the muted repositories out of options GitHub's page sets: they become the mutes (the ones
// not listed any more are unmuted). It returns the options without them.
func (h *Hub) setGitHubMutes(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var opts map[string]json.RawMessage
	if json.Unmarshal(raw, &opts) != nil {
		return raw, nil // the connector says what is wrong
	}
	field, ok := opts[githubMutedRepos]
	if !ok {
		return raw, nil
	}
	var repos []string
	if err := json.Unmarshal(field, &repos); err != nil {
		return raw, nil
	}
	keep := map[string]bool{}
	for _, r := range repos {
		if !strings.Contains(r, "/") {
			return nil, fmt.Errorf("muted repository %q is not owner/name", r)
		}
		keep[strings.ToLower(r)] = true
	}
	if err := h.removeMutes(ctx, func(m InboxMute) bool { return isGitHubRepoMute(m) && !keep[m.Value] }); err != nil {
		return nil, err
	}
	for _, r := range repos {
		if _, _, err := h.addMute(ctx, githubRepoMute(r)); err != nil {
			return nil, err
		}
	}
	opts[githubMutedRepos] = json.RawMessage(`[]`)
	out, _ := json.Marshal(opts)
	return out, nil
}

// listedMutes are the mutes with their source's name.
func (h *Hub) listedMutes() []InboxMute {
	h.inbox.mu.Lock()
	mutes := slices.Clone(h.inbox.mutes)
	h.inbox.mu.Unlock()
	for i := range mutes {
		mutes[i].SourceName = h.sourceName(mutes[i].Source)
	}
	return mutes
}

// sourceName is an inbox source for people: 飞书, 邮件, a connector's name.
func (h *Hub) sourceName(source string) string {
	switch source {
	case "lark":
		return "飞书"
	case "mail":
		return "邮件"
	}
	if c := h.connectors.Get(source); c != nil {
		return c.Name()
	}
	return source
}

var errNoMuteOption = errors.New("this item cannot be muted by that")

func (h *Hub) inboxMuteRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/inbox/mutes", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"mutes": h.listedMutes()})
	})
	// Mute what an item has under a tag ({"item_id", "tag"}): the hub reads the value off the item.
	api.HandleFunc("POST /api/v1/inbox/mutes", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ItemID string `json:"item_id"`
			Tag    string `json:"tag"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		h.inbox.mu.Lock()
		it, ok := h.inbox.items[in.ItemID]
		var source string
		var opt *MuteOption
		if ok {
			source = it.Source
			for _, o := range mutesOf(it) {
				if o.Tag == in.Tag {
					opt = &o
					break
				}
			}
		}
		h.inbox.mu.Unlock()
		if !ok {
			writeErr(w, http.StatusNotFound, errNoInboxItem)
			return
		}
		if opt == nil {
			writeErr(w, http.StatusBadRequest, errNoMuteOption)
			return
		}
		m, settled, err := h.addMute(r.Context(), InboxMute{Source: source, Tag: opt.Tag, Value: opt.Value, Name: opt.Name})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		m.SourceName = h.sourceName(m.Source)
		writeJSON(w, map[string]any{"mute": m, "settled": settled})
	})
	api.HandleFunc("DELETE /api/v1/inbox/mutes/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if err := h.removeMutes(r.Context(), func(m InboxMute) bool { return m.ID == id }); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
}
