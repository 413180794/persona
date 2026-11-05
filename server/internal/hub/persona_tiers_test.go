package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/nwverse/persona/server/internal/extwire"
)

// tierManifestJSON is a tier's persona.json: the package's id, its files under tiers/<key>/.
func tierManifestJSON(id, key string) string {
	return `{"format":1,"id":"` + id + `","name":"奶蛙","about":"NWVERSE 等级","subtitle":"` + key + `",` +
		`"avatar":{"sizes":{"64":"tiers/` + key + `/avatar/64.png"}},` +
		`"dot":{"size":144,"states":{"idle":{"file":"tiers/` + key + `/dot/idle.png"}}}}`
}

// tieredPersona is a tiered package: unhatched (the default, a failed trial), egg (in the trial), tadpole (level 0)
// and cosmic (level 4), its level from the board at boardURL.
func tieredPersona(id, boardURL string) (string, map[string]string) {
	files := map[string]string{}
	levels := []string{}
	for _, t := range []struct{ key, when string }{
		{"unhatched", `{"newbie":"failed"}`}, {"egg", `{"newbie":"pending"}`}, {"tadpole", `{"level":0}`}, {"cosmic", `{"level":4}`},
	} {
		files["tiers/"+t.key+"/avatar/64.png"] = "png-" + t.key
		files["tiers/"+t.key+"/dot/idle.png"] = "apng-" + t.key
		files["tiers/"+t.key+"/persona.json"] = tierManifestJSON(id, t.key)
		levels = append(levels, `{"key":"`+t.key+`","name":"`+t.key+`","when":`+t.when+`,"manifest":"tiers/`+t.key+`/persona.json"}`)
	}
	root := strings.TrimSuffix(tierManifestJSON(id, "unhatched"), "}") +
		`,"tiers":{"source":{"kind":"nwverse-board","url":"` + boardURL + `"},"default":"unhatched","levels":[` +
		strings.Join(levels, ",") + `]}}`
	return root, files
}

// testBoard serves a board with an ETag (a 304 for the same one), or fails.
type testBoard struct {
	srv         *httptest.Server
	mu          sync.Mutex
	body        string
	fail        bool
	notModified int
}

func newTestBoard(t *testing.T) *testBoard {
	b := &testBoard{body: `{"schema":1,"people":[]}`}
	b.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.fail {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		sum := sha256.Sum256([]byte(b.body))
		etag := `"` + hex.EncodeToString(sum[:8]) + `"`
		if r.Header.Get("If-None-Match") == etag {
			b.notModified++
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		_, _ = w.Write([]byte(b.body))
	}))
	t.Cleanup(b.srv.Close)
	return b
}

func (b *testBoard) set(people string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.body = `{"schema":1,"people":[` + people + `]}`
	b.fail = false
}

func (b *testBoard) url() string { return b.srv.URL + "/contribution/data/board.json" }

// shownTier is the tier the list gives a phone for id (its subtitle names it in these packages) and the rev.
func shownTier(t *testing.T, srv *httptest.Server, token, id string) (string, string) {
	t.Helper()
	for _, p := range getPersonas(t, srv, token) {
		if p.ID != id {
			continue
		}
		var m struct {
			Subtitle string          `json:"subtitle"`
			Tiers    json.RawMessage `json:"tiers"`
		}
		if err := json.Unmarshal(p.Manifest, &m); err != nil {
			t.Fatal(err)
		}
		if m.Tiers != nil {
			t.Fatalf("the phone got the tiers block: %s", p.Manifest)
		}
		return m.Subtitle, p.Rev
	}
	t.Fatalf("%s is not listed", id)
	return "", ""
}

// appClient is an app connected to the hub: what is broadcast lands in its queue.
func appClient(h *Hub) *client {
	c := &client{out: make(chan []byte, 64), subs: map[[2]string]bool{}}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	return c
}

func broadcasts(c *client, typ string) int {
	n := 0
	for {
		select {
		case b := <-c.out:
			var m struct{ Type string }
			if json.Unmarshal(b, &m) == nil && m.Type == typ {
				n++
			}
		default:
			return n
		}
	}
}

func TestPersonaTiersFollowTheBoard(t *testing.T) {
	ctx := context.Background()
	h := newTestHub(t)
	board := newTestBoard(t)
	h.tiers.http = board.srv.Client()
	root, files := tieredPersona("nwverse", board.url())
	writePersona(t, h.personasDir(), "nwverse", root, files)
	h.cfg.Personas.GitHubLogin = "Me"
	app := appClient(h)
	srv := httptest.NewServer(h.routes())
	defer srv.Close()
	token := h.cfg.Token

	// Before the board answers: the default tier.
	tier, rev0 := shownTier(t, srv, token, "nwverse")
	if tier != "unhatched" {
		t.Fatalf("tier %q before the board, want unhatched", tier)
	}

	// In the newcomer's trial (the login matched without case): the egg, a new rev, and the apps told.
	board.set(`{"login":"me","level":null,"newbie":{"status":"pending"}}`)
	h.refreshPersonaTiers(ctx)
	tier, rev1 := shownTier(t, srv, token, "nwverse")
	if tier != "egg" || rev1 == rev0 {
		t.Fatalf("tier %q rev %s (was %s), want egg and a new rev", tier, rev1, rev0)
	}
	if n := broadcasts(app, "personas"); n != 1 {
		t.Fatalf("%d personas broadcasts, want 1", n)
	}

	// The board unchanged: a 304, the same tier, nothing broadcast.
	h.refreshPersonaTiers(ctx)
	if tier, rev := shownTier(t, srv, token, "nwverse"); tier != "egg" || rev != rev1 || board.notModified != 1 {
		t.Fatalf("tier %q rev %s, %d 304s", tier, rev, board.notModified)
	}
	if n := broadcasts(app, "personas"); n != 0 {
		t.Fatalf("%d broadcasts for an unchanged board", n)
	}

	// Level 2, which the package has no tier for: the highest at or under it.
	board.set(`{"login":"someone","level":4,"newbie":{"status":"passed"}},{"login":"me","level":2,"newbie":{"status":"passed"}}`)
	h.refreshPersonaTiers(ctx)
	if tier, _ := shownTier(t, srv, token, "nwverse"); tier != "tadpole" {
		t.Fatalf("level 2: tier %q, want tadpole", tier)
	}
	// Above every tier: the highest.
	board.set(`{"login":"me","level":5,"newbie":{"status":"passed"}}`)
	h.refreshPersonaTiers(ctx)
	if tier, _ := shownTier(t, srv, token, "nwverse"); tier != "cosmic" {
		t.Fatalf("level 5: tier %q, want cosmic", tier)
	}

	// The board out of reach: the tier stays.
	board.mu.Lock()
	board.fail = true
	board.mu.Unlock()
	h.refreshPersonaTiers(ctx)
	if tier, _ := shownTier(t, srv, token, "nwverse"); tier != "cosmic" {
		t.Fatalf("board down: tier %q, want cosmic still", tier)
	}

	// The tier's files are served (the phone downloads the ones its manifest names).
	if code, body := getPersonaFile(t, srv, token, "nwverse/tiers/cosmic/dot/idle.png"); code != 200 || body != "apng-cosmic" {
		t.Fatalf("tier file: %d %q", code, body)
	}

	// Not on the board: the default.
	board.set(`{"login":"someone","level":4,"newbie":{"status":"passed"}}`)
	h.refreshPersonaTiers(ctx)
	if tier, _ := shownTier(t, srv, token, "nwverse"); tier != "unhatched" {
		t.Fatalf("not on the board: tier %q, want unhatched", tier)
	}
}

// Without a GitHub login the tiered look shows its default, whatever it showed before.
func TestPersonaTiersWithoutLogin(t *testing.T) {
	h := newTestHub(t)
	board := newTestBoard(t)
	h.tiers.http = board.srv.Client()
	root, files := tieredPersona("nwverse", board.url())
	writePersona(t, h.personasDir(), "nwverse", root, files)
	h.tiers.seen = map[string]tierSeen{"nwverse": {Tier: "cosmic", Login: "me"}}
	srv := httptest.NewServer(h.routes())
	defer srv.Close()
	if tier, _ := shownTier(t, srv, h.cfg.Token, "nwverse"); tier != "cosmic" {
		t.Fatalf("tier %q, want the one seen last", tier)
	}
	// The GitHub connector is the manager's: away, it can't say, and the tier stays.
	h.refreshPersonaTiers(context.Background())
	if tier, _ := shownTier(t, srv, h.cfg.Token, "nwverse"); tier != "cosmic" {
		t.Fatalf("manager away: tier %q, want the one seen last", tier)
	}
	// It says GitHub is not signed in.
	extTokensOf(t, h).on(extwire.HubGitHubLogin, func(json.RawMessage) (any, error) {
		return map[string]any{"login": "", "known": true}, nil
	})
	h.refreshPersonaTiers(context.Background())
	if tier, _ := shownTier(t, srv, h.cfg.Token, "nwverse"); tier != "unhatched" {
		t.Fatalf("no login: tier %q, want unhatched", tier)
	}
}

// The tier last seen is kept, so a restarted hub shows it before the board answers.
func TestPersonaTierKeptAcrossRestart(t *testing.T) {
	ctx := context.Background()
	store := &settingsStore{vals: map[string][]byte{}}
	board := newTestBoard(t)
	h := newTestHub(t)
	h.store = store
	h.tiers.http = board.srv.Client()
	h.cfg.Personas.GitHubLogin = "me"
	root, files := tieredPersona("nwverse", board.url())
	writePersona(t, h.personasDir(), "nwverse", root, files)
	board.set(`{"login":"me","level":0,"newbie":{"status":"passed"}}`)
	h.refreshPersonaTiers(ctx)

	again := newTestHub(t)
	again.cfg.dir = h.cfg.dir
	again.store = store
	again.loadPersonaTiers(ctx)
	srv := httptest.NewServer(again.routes())
	defer srv.Close()
	if tier, _ := shownTier(t, srv, again.cfg.Token, "nwverse"); tier != "tadpole" {
		t.Fatalf("after a restart: tier %q, want tadpole", tier)
	}
}

// A tier whose files are not all there leaves the phone on the default tier rather than half a look.
func TestPersonaTierThatDoesNotCheckOut(t *testing.T) {
	h := newTestHub(t)
	root, files := tieredPersona("nwverse", "https://board.example/board.json")
	delete(files, "tiers/cosmic/dot/idle.png")
	writePersona(t, h.personasDir(), "nwverse", root, files)
	h.tiers.seen = map[string]tierSeen{"nwverse": {Tier: "cosmic"}}
	srv := httptest.NewServer(h.routes())
	defer srv.Close()
	if tier, _ := shownTier(t, srv, h.cfg.Token, "nwverse"); tier != "unhatched" {
		t.Fatalf("tier %q, want the default", tier)
	}
}

func TestPersonaTiersRules(t *testing.T) {
	h := newTestHub(t)
	dir := h.personasDir()
	cases := map[string]func(root string, files map[string]string) (string, map[string]string){
		"plainhttp": func(root string, files map[string]string) (string, map[string]string) {
			return strings.Replace(root, "https://", "http://", 1), files
		},
		"otherkind": func(root string, files map[string]string) (string, map[string]string) {
			return strings.Replace(root, "nwverse-board", "leaderboard", 1), files
		},
		"nodefault": func(root string, files map[string]string) (string, map[string]string) {
			return strings.Replace(root, `"default":"unhatched"`, `"default":"nope"`, 1), files
		},
		"brokendefault": func(root string, files map[string]string) (string, map[string]string) {
			delete(files, "tiers/unhatched/persona.json")
			return root, files
		},
	}
	for id, change := range cases {
		root, files := tieredPersona(id, "https://board.example/board.json")
		root, files = change(root, files)
		writePersona(t, dir, id, root, files)
	}
	root, files := tieredPersona("good", "https://board.example/board.json")
	writePersona(t, dir, "good", root, files)
	srv := httptest.NewServer(h.routes())
	defer srv.Close()
	list := getPersonas(t, srv, h.cfg.Token)
	if len(list) != 1 || list[0].ID != "good" {
		t.Fatalf("list = %+v, want only good", list)
	}
}

func TestPersonaBuiltinsAndDirectories(t *testing.T) {
	h := newTestHub(t)
	builtin := fstest.MapFS{}
	add := func(fsys fstest.MapFS, id, name string) {
		m := strings.Replace(personaManifestJSON(id), `"豆豆"`, `"`+name+`"`, 1)
		fsys[id+"/persona.json"] = &fstest.MapFile{Data: []byte(m)}
		for f, body := range personaFilesOK() {
			fsys[id+"/"+f] = &fstest.MapFile{Data: []byte(body)}
		}
	}
	add(builtin, "nwverse", "内置")
	add(builtin, "second", "第二个")
	h.builtinLooks = builtin
	writePersona(t, h.personasDir(), "doudou", personaManifestJSON("doudou"), personaFilesOK())
	srv := httptest.NewServer(h.routes())
	defer srv.Close()

	names := func() []string {
		var out []string
		for _, p := range getPersonas(t, srv, h.cfg.Token) {
			var m struct{ Name string }
			_ = json.Unmarshal(p.Manifest, &m)
			out = append(out, p.ID+":"+m.Name)
		}
		return out
	}
	// The built-in ones first, then the directories.
	if got := strings.Join(names(), ","); got != "nwverse:内置,second:第二个,doudou:豆豆" {
		t.Fatalf("list = %s", got)
	}
	if code, body := getPersonaFile(t, srv, h.cfg.Token, "nwverse/dot/idle.png"); code != 200 || body != "png-i" {
		t.Fatalf("built-in file: %d %q", code, body)
	}

	// A directory of the same id takes the built-in one's place (a look tried on one hub before it ships).
	writePersona(t, h.personasDir(), "nwverse", strings.Replace(personaManifestJSON("nwverse"), `"豆豆"`, `"试的"`, 1), personaFilesOK())
	if got := strings.Join(names(), ","); got != "nwverse:试的,second:第二个,doudou:豆豆" {
		t.Fatalf("with a directory: %s", got)
	}
	// One that does not check out does not hide the built-in one.
	if err := os.Remove(filepath.Join(h.personasDir(), "nwverse", "dot", "idle.png")); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(names(), ","); got != "nwverse:内置,second:第二个,doudou:豆豆" {
		t.Fatalf("with a broken directory: %s", got)
	}
	if code, body := getPersonaFile(t, srv, h.cfg.Token, "nwverse/dot/idle.png"); code != 200 || body != "png-i" {
		t.Fatalf("the built-in file again: %d %q", code, body)
	}
}

// personad's own packages check out (the generated ones in personas/, if any).
func TestPersonaBuiltinsShipped(t *testing.T) {
	h := newTestHub(t)
	h.builtinLooks = builtinPersonaFS()
	for _, id := range h.builtinPersonaIDs() {
		p, err := openPersona(mustSub(h.builtinLooks, id), id)
		if err != nil {
			t.Fatalf("built-in %s: %v", id, err)
		}
		if p.m.Tiers != nil {
			for _, tier := range p.m.Tiers.Levels {
				if _, _, err := p.tierManifest(&tier); err != nil {
					t.Fatalf("built-in %s tier %s: %v", id, tier.Key, err)
				}
			}
		}
	}
}
