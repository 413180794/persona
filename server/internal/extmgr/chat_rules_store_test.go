package extmgr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

// settingsOnly hides all but the settings of a store: a manager on it has no ledger, so a test against the real database
// does not write rows into it.
type settingsOnly struct{ Store }

// onRuleSettings runs f once with the settings in memory and, when PERSONA_TEST_DATABASE_URL is set
// (scripts/test/test-server.sh), once on Postgres with the manager's database: the rules' setting cleared before and
// after. manager makes a new manager on that store, as a restart does.
func onRuleSettings(t *testing.T, f func(t *testing.T, store Store, manager func() *Manager)) {
	run := func(t *testing.T, store Store) {
		f(t, store, func() *Manager {
			h := newTestManager(t)
			h.store = store
			return h
		})
	}
	t.Run("memory", func(t *testing.T) { run(t, settingsOnly{newKeepStore()}) })
	t.Run("postgres", func(t *testing.T) {
		url := os.Getenv("PERSONA_TEST_DATABASE_URL")
		if url == "" {
			t.Skip("PERSONA_TEST_DATABASE_URL not set (scripts/test/test-server.sh runs this on the hub machine)")
		}
		database, err := db.Open(context.Background(), url, db.ManagerSet)
		if err != nil {
			t.Fatal(err)
		}
		clear := func() {
			var gone json.RawMessage
			if _, err := database.TakeSetting(context.Background(), chatRulesKey, &gone); err != nil {
				t.Error(err)
			}
		}
		clear()
		t.Cleanup(func() {
			clear()
			database.Close()
		})
		run(t, settingsOnly{database})
	})
}

func ghUserRule(n int) connector.UserRule {
	rule, err := connector.ChatRuleFor("github", "github_comment", json.RawMessage(fmt.Sprintf(`{"ref":"example-org/example-repo#%d"}`, n)))
	if err != nil {
		panic(err)
	}
	return rule
}

func larkUserRule(conv string) connector.UserRule {
	rule, err := connector.ChatRuleFor("lark", "POST /conversations/"+conv+"/messages", json.RawMessage(`{"conversation":"`+conv+`"}`))
	if err != nil {
		panic(err)
	}
	return rule
}

func idsOf(list []connector.UserRule) []string {
	var out []string
	for _, u := range list {
		out = append(out, u.ID())
	}
	return out
}

// The setting keeps the rules in the order they were added, one to a chat: adding a chat again changes nothing, and
// dropping one that is not there says so.
func TestChatRuleStoreKeepsWhatWasAdded(t *testing.T) {
	ctx := context.Background()
	onRuleSettings(t, func(t *testing.T, _ Store, manager func() *Manager) {
		s := manager().ruleStore
		if list, err := s.List(ctx); err != nil || len(list) != 0 {
			t.Fatalf("nothing kept yet: %v, %v", list, err)
		}
		first := ghUserRule(12)
		first.Row, first.Summary = 7, "comment on #12"
		for _, u := range []connector.UserRule{first, larkUserRule("oc_1"), ghUserRule(13)} {
			if err := s.Add(ctx, u); err != nil {
				t.Fatal(err)
			}
		}
		dup := ghUserRule(12)
		dup.Row = 99
		if err := s.Add(ctx, dup); err != nil {
			t.Fatal(err)
		}
		list, err := s.List(ctx)
		if err != nil || !slices.Equal(idsOf(list), []string{"github:example-org/example-repo#12", "lark:oc_1", "github:example-org/example-repo#13"}) {
			t.Fatalf("kept %v, %v", idsOf(list), err)
		}
		if list[0].Row != 7 || list[0].Summary != "comment on #12" {
			t.Fatalf("the first rule of a chat is kept as it was made: %+v", list[0])
		}
		if had, err := s.Remove(ctx, "lark:oc_1"); err != nil || !had {
			t.Fatalf("remove: %v %v", had, err)
		}
		if had, err := s.Remove(ctx, "lark:oc_1"); err != nil || had {
			t.Fatalf("remove again: %v %v", had, err)
		}
		if list, _ := s.List(ctx); !slices.Equal(idsOf(list), []string{"github:example-org/example-repo#12", "github:example-org/example-repo#13"}) {
			t.Fatalf("after dropping one: %v", idsOf(list))
		}
		// What is kept is plain JSON: a rule read back is the rule that was written, numbers as the gate reads them.
		if err := manager().ruleStore.Add(ctx, ghUserRule(14)); err != nil {
			t.Fatal(err)
		}
		if list, _ := s.List(ctx); len(list) != 3 || list[2].Validate() != nil {
			t.Fatalf("read back by another manager: %+v", list)
		}
	})
}

// A setting that is not a list of rules, or a list none of whose elements is one, is taken as none, said in the log: the
// gate asks, which is where it started. The next change writes over it.
func TestChatRuleStoreReadsBadDataAsNone(t *testing.T) {
	ctx := context.Background()
	for name, bad := range map[string]any{
		"a string":                        "nonsense",
		"an object":                       map[string]any{"rules": []any{}},
		"a list of numbers":               []any{1, 2},
		"a rule that is not an object":    []any{map[string]any{"rule": 5}},
		"a rule with a text for the time": []any{map[string]any{"rule": map[string]any{"connector": "github"}, "at": 12}},
	} {
		onRuleSettings(t, func(t *testing.T, store Store, manager func() *Manager) {
			var logged []string
			h := manager()
			h.opts.Logf = func(f string, a ...any) { logged = append(logged, fmt.Sprintf(f, a...)) }
			if err := store.PutSetting(ctx, chatRulesKey, bad); err != nil {
				t.Fatal(err)
			}
			list, err := h.ruleStore.List(ctx)
			if err != nil || len(list) != 0 {
				t.Fatalf("%s: read as %+v, %v", name, list, err)
			}
			if !slices.ContainsFunc(logged, func(l string) bool { return strings.Contains(l, chatRulesKey) }) {
				t.Errorf("%s: nothing said in the log: %v", name, logged)
			}
			// At start: no user rule, so the gate asks.
			h.startConnectorGate(ctx)
			if len(h.connectors.UserRules()) != 0 {
				t.Errorf("%s: the gate has %+v", name, h.connectors.UserRules())
			}
			// The next change writes over it.
			if _, _, err := h.addChatRule(ctx, githubRowOf("github_comment", chatIssue13)); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if list, err := h.ruleStore.List(ctx); err != nil || !slices.Equal(idsOf(list), []string{"github:example-org/example-repo#13"}) {
				t.Errorf("%s: after the first change %v, %v", name, idsOf(list), err)
			}
		})
	}
}

// A slip in a setting someone edited by hand costs only the element that is no rule: it is left out one by one and said in
// the log, the good rules around it are read and kept, and the next write is the list without it, with them in it.
func TestChatRuleStoreLeavesOutOnlyTheElementsThatAreNoRule(t *testing.T) {
	ctx := context.Background()
	good12, _ := json.Marshal(ghUserRule(12))
	lark1, _ := json.Marshal(larkUserRule("oc_1"))
	g12, l1 := string(good12), string(lark1)
	for name, c := range map[string]struct {
		setting []string
		bad     int // how many elements are no rule
		want    []string
	}{
		"a good rule and a number":              {[]string{g12, `5`}, 1, []string{"github:example-org/example-repo#12"}},
		"a number and a good rule":              {[]string{`5`, g12}, 1, []string{"github:example-org/example-repo#12"}},
		"a rule that is not an object between":  {[]string{g12, `{"rule": 5}`, l1}, 1, []string{"github:example-org/example-repo#12", "lark:oc_1"}},
		"a time that is a number, and a string": {[]string{`{"rule": {"connector": "github"}, "at": 12}`, g12, `"x"`}, 2, []string{"github:example-org/example-repo#12"}},
	} {
		onRuleSettings(t, func(t *testing.T, store Store, manager func() *Manager) {
			var logged []string
			h := manager()
			h.opts.Logf = func(f string, a ...any) { logged = append(logged, fmt.Sprintf(f, a...)) }
			var elements []json.RawMessage
			for _, e := range c.setting {
				elements = append(elements, json.RawMessage(e))
			}
			if err := store.PutSetting(ctx, chatRulesKey, elements); err != nil {
				t.Fatal(err)
			}
			list, err := h.ruleStore.List(ctx)
			if err != nil || !slices.Equal(idsOf(list), c.want) {
				t.Fatalf("%s: read as %v, %v; want %v", name, idsOf(list), err, c.want)
			}
			if n := countLines(logged, "is not a rule"); n != c.bad {
				t.Errorf("%s: %d elements said in the log, want %d: %v", name, n, c.bad, logged)
			}
			// At start the good rules are in force.
			h.startConnectorGate(ctx)
			if got := h.connectors.UserRules(); len(got) != len(c.want) {
				t.Errorf("%s: the gate has %+v", name, got)
			}
			// The next change keeps them, and the elements that were no rule are gone.
			if _, _, err := h.addChatRule(ctx, githubRowOf("github_comment", chatIssue13)); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			var raw []json.RawMessage
			if _, err := store.Setting(ctx, chatRulesKey, &raw); err != nil || len(raw) != len(c.want)+1 {
				t.Errorf("%s: the setting holds %d elements after the change: %s, %v", name, len(raw), raw, err)
			}
			if list, err := h.ruleStore.List(ctx); err != nil || !slices.Equal(idsOf(list), append(slices.Clone(c.want), "github:example-org/example-repo#13")) {
				t.Errorf("%s: after the change %v, %v", name, idsOf(list), err)
			}
			if len(h.connectors.UserRules()) != len(c.want)+1 {
				t.Errorf("%s: the gate has %+v after the change", name, h.connectors.UserRules())
			}
		})
	}
}

func countLines(lines []string, part string) (n int) {
	for _, l := range lines {
		if strings.Contains(l, part) {
			n++
		}
	}
	return n
}

// unreadableStore is a store whose reads can fail, and that counts the writes it was asked for.
type unreadableStore struct {
	Store
	failRead atomic.Bool
	puts     atomic.Int32
}

func (s *unreadableStore) Setting(ctx context.Context, key string, out any) (bool, error) {
	if s.failRead.Load() {
		return false, errors.New("the database went away")
	}
	return s.Store.Setting(ctx, key, out)
}

func (s *unreadableStore) PutSetting(ctx context.Context, key string, v any) error {
	s.puts.Add(1)
	return s.Store.PutSetting(ctx, key, v)
}

// A database that cannot be read is not "no rules" for a change: writing a list of one over it would lose the rest. At
// start it is no rules (the gate asks) and said; the first change after it can be read again reads what was there, and
// keeps it.
func TestChatRuleStoreNeverWritesOverWhatItCouldNotRead(t *testing.T) {
	ctx := context.Background()
	kept := newKeepStore()
	flaky := &unreadableStore{Store: kept}
	h := newTestManager(t)
	h.store = flaky
	if err := h.ruleStore.Add(ctx, ghUserRule(12)); err != nil {
		t.Fatal(err)
	}
	puts := flaky.puts.Load()

	flaky.failRead.Store(true)
	if _, err := h.ruleStore.List(ctx); err == nil {
		t.Error("an unreadable database listed as none")
	}
	if err := h.ruleStore.Add(ctx, ghUserRule(13)); err == nil || flaky.puts.Load() != puts {
		t.Errorf("an add on a database that cannot be read: %v, %d writes after %d", err, flaky.puts.Load(), puts)
	}
	if _, err := h.ruleStore.Remove(ctx, "github:example-org/example-repo#12"); err == nil || flaky.puts.Load() != puts {
		t.Errorf("a removal on a database that cannot be read: %v, %d writes after %d", err, flaky.puts.Load(), puts)
	}

	// A manager that starts on it has no user rule, the gate asks; once it reads, the first change keeps the old rules.
	h2 := newTestManager(t)
	h2.store = flaky
	h2.startConnectorGate(ctx)
	gh := &chatGitHub{}
	if err := h2.addConnector(gh); err != nil {
		t.Fatal(err)
	}
	yes := true
	hubOf(h2).answer = func(p proto.Interaction) proto.Resolution { return proto.Resolution{Allow: &yes} }
	if len(h2.connectors.UserRules()) != 0 || !chatWrite(t, h2, gh, "github_comment", chatIssue12) {
		t.Fatal("a rule was in force with the database unreadable")
	}
	if _, _, err := h2.addChatRule(ctx, githubRowOf("github_comment", chatIssue13)); err == nil {
		t.Fatal("a change was made on rules that were never read")
	}
	flaky.failRead.Store(false)
	if _, _, err := h2.addChatRule(ctx, githubRowOf("github_comment", chatIssue13)); err != nil {
		t.Fatal(err)
	}
	var list []connector.UserRule
	if _, err := kept.Setting(ctx, chatRulesKey, &list); err != nil || !slices.Equal(idsOf(list), []string{"github:example-org/example-repo#12", "github:example-org/example-repo#13"}) {
		t.Fatalf("kept %v, %v", idsOf(list), err)
	}
	if len(h2.connectors.UserRules()) != 2 || chatWrite(t, h2, gh, "github_comment", chatIssue12) {
		t.Fatal("the rules that could be read again are not in force")
	}
}

// There are at most chatRulesMost rules: one more is refused with its own error and nothing is written; a chat that has
// a rule can be added again at the limit, and dropping one makes room.
func TestChatRuleStoreHasALimit(t *testing.T) {
	ctx := context.Background()
	onRuleSettings(t, func(t *testing.T, store Store, manager func() *Manager) {
		var full []connector.UserRule
		for n := 1; n <= chatRulesMost; n++ {
			full = append(full, ghUserRule(n))
		}
		if err := store.PutSetting(ctx, chatRulesKey, full); err != nil {
			t.Fatal(err)
		}
		s := manager().ruleStore
		if err := s.Add(ctx, ghUserRule(chatRulesMost+1)); !errors.Is(err, errChatRulesFull) {
			t.Fatalf("the %dst: %v", chatRulesMost+1, err)
		}
		if err := s.Add(ctx, ghUserRule(1)); err != nil {
			t.Fatalf("a chat that has a rule, at the limit: %v", err)
		}
		if list, _ := s.List(ctx); len(list) != chatRulesMost {
			t.Fatalf("%d rules after the refused one", len(list))
		}
		if had, err := s.Remove(ctx, ghUserRule(3).ID()); err != nil || !had {
			t.Fatal(had, err)
		}
		if err := s.Add(ctx, ghUserRule(chatRulesMost+1)); err != nil {
			t.Fatalf("with room: %v", err)
		}
		if list, _ := s.List(ctx); len(list) != chatRulesMost {
			t.Fatalf("%d rules", len(list))
		}
	})
}

// The store takes only a rule of one chat that allows, whoever asks: nothing else is written.
func TestChatRuleStoreRefusesWhatIsNotOneChat(t *testing.T) {
	ctx := context.Background()
	onRuleSettings(t, func(t *testing.T, store Store, manager func() *Manager) {
		s := manager().ruleStore
		for name, u := range map[string]connector.UserRule{
			"everything":   {Rule: connector.Rule{Connector: "*", Action: "*", Decision: connector.Allow}},
			"every chat":   {Rule: connector.Rule{Connector: "lark", Action: "POST /conversations/*/messages", Decision: connector.Allow}},
			"a deny":       {Rule: connector.Rule{Connector: "lark", Action: "POST /conversations/oc_1/messages", Decision: connector.Deny}},
			"nothing":      {},
			"a repository": {Rule: connector.Rule{Connector: "github", Action: "github_comment", Match: map[string]any{"repo": "example-org/example-repo"}, Decision: connector.Allow}},
		} {
			if err := s.Add(ctx, u); err == nil {
				t.Errorf("%s was kept", name)
			}
		}
		if list, err := s.List(ctx); err != nil || len(list) != 0 {
			t.Fatalf("kept %v, %v", list, err)
		}
	})
}

// Adds from several requests at once are all kept: the read, the change and the write of one are not interleaved with
// another's.
func TestChatRuleStoreAddsAtOnce(t *testing.T) {
	ctx := context.Background()
	onRuleSettings(t, func(t *testing.T, _ Store, manager func() *Manager) {
		s := manager().ruleStore
		var wg sync.WaitGroup
		for n := 1; n <= 40; n++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := s.Add(ctx, ghUserRule(n)); err != nil {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
		if list, err := s.List(ctx); err != nil || len(list) != 40 {
			t.Fatalf("%d rules kept after 40 adds: %v", len(list), err)
		}
	})
}

// With no database at all the store says so; a manager with none starts all the same, with no user rule.
func TestChatRuleStoreWithoutADatabase(t *testing.T) {
	ctx := context.Background()
	h := newTestManager(t)
	if _, err := h.ruleStore.List(ctx); !errors.Is(err, errNoDatabase) {
		t.Errorf("list: %v", err)
	}
	if err := h.ruleStore.Add(ctx, ghUserRule(12)); !errors.Is(err, errNoDatabase) {
		t.Errorf("add: %v", err)
	}
	if _, err := h.ruleStore.Remove(ctx, "github:x/y#1"); !errors.Is(err, errNoDatabase) {
		t.Errorf("remove: %v", err)
	}
	h.startConnectorGate(ctx)
	if h.connectors == nil || len(h.connectors.UserRules()) != 0 {
		t.Errorf("the gate of a manager with no database: %+v", h.connectors)
	}
}

// What the user made is still there after the manager restarts: a new manager on the same database reads it, applies it
// (the gate lets the chat through without a card, the node is served the Lark rule) and so on through the next restart;
// a rule dropped stays dropped.
func TestChatRulesSurviveARestart(t *testing.T) {
	ctx := context.Background()
	onRuleSettings(t, func(t *testing.T, _ Store, manager func() *Manager) {
		start := func() (*Manager, *chatGitHub) {
			h := manager()
			withLarkGates(t, h, chatLarkCommand)
			h.startConnectorGate(ctx) // what Run does first
			gh := &chatGitHub{}
			if err := h.addConnector(gh); err != nil {
				t.Fatal(err)
			}
			yes := true
			hubOf(h).answer = func(proto.Interaction) proto.Resolution { return proto.Resolution{Allow: &yes} }
			return h, gh
		}
		served := func(h *Manager) []proto.LarkRule {
			lk, err := h.assistantLark(ctx)
			if err != nil {
				t.Fatal(err)
			}
			return lk.Rules
		}
		lark := db.SentAsUser{ID: 4, Connector: "lark", Action: "POST /conversations/oc_1/messages", Target: json.RawMessage(`{"command":"feed","conversation":"oc_1"}`)}

		h1, gh1 := start()
		if !chatWrite(t, h1, gh1, "github_comment", chatIssue12) {
			t.Fatal("asked nothing with no rule")
		}
		if _, _, err := h1.addChatRule(ctx, githubRowOf("github_comment", chatIssue12)); err != nil {
			t.Fatal(err)
		}
		if _, _, err := h1.addChatRule(ctx, lark); err != nil {
			t.Fatal(err)
		}

		h2, gh2 := start() // the restart
		if chatWrite(t, h2, gh2, "github_comment", chatIssue12) || !chatWrite(t, h2, gh2, "github_comment", chatIssue13) {
			t.Error("after a restart the chat of the rule is asked about, or another is not")
		}
		if got := served(h2); !reflect.DeepEqual(got, []proto.LarkRule{{Method: "POST", Path: "/conversations/oc_1/messages", Action: "allow"}}) {
			t.Errorf("after a restart the node is served %+v", got)
		}
		if rules, err := h2.chatRules(ctx); err != nil || len(rules) != 2 || rules[0].Row == 0 && rules[1].Row == 0 || rules[0].At.IsZero() {
			t.Errorf("what the list shows after a restart: %+v, %v", rules, err)
		}
		if had, err := h2.removeChatRule(ctx, "github:example-org/example-repo#12"); err != nil || !had {
			t.Fatal(had, err)
		}

		h3, gh3 := start()
		if !chatWrite(t, h3, gh3, "github_comment", chatIssue12) {
			t.Error("a rule dropped came back after a restart")
		}
		if got := served(h3); len(got) != 1 {
			t.Errorf("the Lark rule that was not dropped: %+v", got)
		}
	})
}

// A rule dropped is dropped whole: every entry of that chat goes, however it got there (a setting edited by hand can
// hold a chat twice), and the other chats stay in their order.
func TestChatRuleStoreDropsEveryEntryOfAChat(t *testing.T) {
	ctx := context.Background()
	onRuleSettings(t, func(t *testing.T, store Store, manager func() *Manager) {
		if err := store.PutSetting(ctx, chatRulesKey, []connector.UserRule{ghUserRule(1), ghUserRule(2), ghUserRule(1), ghUserRule(3)}); err != nil {
			t.Fatal(err)
		}
		s := manager().ruleStore
		if had, err := s.Remove(ctx, ghUserRule(1).ID()); err != nil || !had {
			t.Fatal(had, err)
		}
		if list, _ := s.List(ctx); !slices.Equal(idsOf(list), []string{ghUserRule(2).ID(), ghUserRule(3).ID()}) {
			t.Fatalf("kept %v", idsOf(list))
		}
	})
}
