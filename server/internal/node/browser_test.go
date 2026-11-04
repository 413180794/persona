package node

import (
	"context"
	"encoding/json"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeEgoHome is a home with ego's first-run setup done (Local State, ego-browser) or not, and the statistics' consent.
func fakeEgoHome(t *testing.T, setUp, imported, stats bool, cli string) string {
	t.Helper()
	home := t.TempDir()
	data := filepath.Join(home, "Library", "Application Support", "Citro Labs", "ego lite")
	if err := os.MkdirAll(data, 0o700); err != nil {
		t.Fatal(err)
	}
	if setUp {
		n := 0
		if imported {
			n = 1
		}
		state := `{"ego":{"welcome_page_shown":true,"onboarding_imported_browser_data":` + map[bool]string{true: "1", false: "0"}[n == 1] + `}}`
		_ = os.WriteFile(filepath.Join(data, "Local State"), []byte(state), 0o600)
	}
	if stats {
		_ = os.WriteFile(filepath.Join(data, "Consent To Send Stats"), []byte("00000000-0000-4000-8000-000000000000"), 0o600)
	}
	if cli != "" {
		bin := filepath.Join(home, ".local", "bin")
		_ = os.MkdirAll(bin, 0o700)
		_ = os.WriteFile(filepath.Join(bin, "ego-browser"), []byte(cli), 0o755)
	}
	old := egoHome
	egoHome = func() string { return home }
	t.Cleanup(func() { egoHome = old })
	return home
}

func TestEgoSetUpIsReadFromItsLocalState(t *testing.T) {
	fakeEgoHome(t, true, true, true, "#!/bin/sh\n")
	if setUp, imported, analytics := egoSetUp(); !setUp || !imported || analytics != "on" {
		t.Fatal(setUp, imported, analytics)
	}
	fakeEgoHome(t, true, false, false, "")
	if setUp, _, analytics := egoSetUp(); setUp || analytics != "off" {
		t.Fatal("no ego-browser yet: not set up", setUp, analytics)
	}
	fakeEgoHome(t, false, false, false, "#!/bin/sh\n")
	if setUp, _, analytics := egoSetUp(); setUp || analytics != "unknown" {
		t.Fatal("never opened", setUp, analytics)
	}
}

func TestBrowserScriptWrapsTheAssistants(t *testing.T) {
	s := browserScript("persona-1a2b3c4d", "await page.goto(\"https://example.com\");\nreturn 1;", false, false, true, "/tmp/x/page.png")
	for _, want := range []string{`taskSpace("persona-1a2b3c4d")`, `task.page("p1")`, "return 1;", `page.screenshot({ path: "/tmp/x/page.png" })`, "__r.error"} {
		if !strings.Contains(s, want) {
			t.Errorf("no %q in\n%s", want, s)
		}
	}
	look := browserScript("persona-1", "", true, true, true, "/tmp/x/page.png")
	if !strings.Contains(look, `page.snapshot({ scope: "full_page" })`) || strings.Contains(look, "__r.value") {
		t.Fatal(look)
	}
	text, r := splitBrowserOutput("hello\nworld\n" + browserMark + `{"value":2,"url":"https://example.com/"}` + "\n")
	if text != "hello\nworld" || r["value"] != float64(2) || r["url"] != "https://example.com/" {
		t.Fatal(text, r)
	}
	if _, r := splitBrowserOutput("Error: boom"); r != nil {
		t.Fatal("no result line")
	}
	long, _ := json.Marshal(map[string]any{"snapshot": strings.Repeat("界", browserMost)})
	if c := clipJSON(long); len(c) > browserMost+400 || !json.Valid(c) {
		t.Fatalf("%d bytes, valid %v", len(c), json.Valid(c))
	}
}

// browser.call against a fake ego-browser that runs nothing: it answers what the wrapped script would print.
func TestBrowserCall(t *testing.T) {
	cli := `#!/bin/sh
script=$(cat)
case "$script" in *throwme*) echo "TypeError: nope" >&2; exit 1 ;; esac
printf 'log line\n'
printf '@@persona-browser@@{"value":"done","url":"https://example.com/","title":"Example Domain"}\n'
`
	fakeEgoHome(t, true, false, false, cli)
	res, err := browserCall(context.Background(), json.RawMessage(`{"session": "persona-1", "script": "return 'done'"}`))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if json.Unmarshal(res.Output, &out) != nil || out["value"] != "done" || out["console"] != "log line" || out["title"] != "Example Domain" {
		t.Fatalf("%s", res.Output)
	}
	if _, err := browserCall(context.Background(), json.RawMessage(`{"session": "persona-1", "script": "throwme"}`)); err == nil || !strings.Contains(err.Error(), "TypeError: nope") {
		t.Fatalf("what it said: %v", err)
	}
	if _, err := browserCall(context.Background(), json.RawMessage(`{"session": "persona-1", "script": "  "}`)); err == nil {
		t.Fatal("an empty script")
	}
	fakeEgoHome(t, false, false, false, "")
	if _, err := browserCall(context.Background(), json.RawMessage(`{"session": "persona-1", "look": true}`)); err == nil {
		t.Fatal("no ego-browser")
	}
}

// The real ego lite on this Mac (PERSONA_EGO_E2E=1, its first-run setup done): the installed app passes the install's
// checks, status says ready, a look at example.com in a task space of its own brings back its snapshot and a
// screenshot, and the space is closed after.
func TestEgoEndToEnd(t *testing.T) {
	if os.Getenv("PERSONA_EGO_E2E") == "" {
		t.Skip("needs ego lite set up on this Mac: PERSONA_EGO_E2E=1")
	}
	ctx := context.Background()
	if app := egoApp(); app == "" {
		t.Fatal("not installed")
	} else if err := verifyEgoApp(ctx, app); err != nil {
		t.Fatal("the installed app fails the install's checks:", err)
	}
	st := browserStatus(ctx)
	t.Logf("status: %+v", st)
	if !st.Ready {
		t.Fatalf("not ready: %s", st.Note)
	}
	const session = "persona-e2e-test"
	defer closeSpace(t, `const t = await taskSpace("`+session+`"); await t.finish({ keep: [] });`)
	if _, err := browserCall(ctx, json.RawMessage(`{"session": "`+session+`", "script": "await page.goto(\"https://example.com\"); return await page.title();", "screenshot": true}`)); err != nil {
		t.Fatal(err)
	}
	res, err := browserCall(ctx, json.RawMessage(`{"session": "`+session+`", "look": true}`))
	if err != nil || !strings.Contains(string(res.Output), "Example Domain") || res.Image == nil {
		t.Fatalf("%v %.300s", err, res.Output)
	}
	t.Logf("look: %d bytes of JSON, screenshot %s %dx%d", len(res.Output), res.Image.Mime, res.Image.Width, res.Image.Height)
}

func closeSpace(t *testing.T, script string) {
	t.Helper()
	if out, err := osexec.Command(egoBin(), "nodejs", "-e", script).CombinedOutput(); err != nil {
		t.Logf("closing the task space: %v %s", err, out)
	}
}

// The real installer (PERSONA_EGO_INSTALLER_CHECK=1, a Mac): ego's DMG for this chip downloads, mounts, and the app in
// it passes the install's checks. Nothing is installed.
func TestEgoInstallerCheck(t *testing.T) {
	if os.Getenv("PERSONA_EGO_INSTALLER_CHECK") == "" {
		t.Skip("downloads ego's DMG: PERSONA_EGO_INSTALLER_CHECK=1")
	}
	var steps []string
	app, done, err := fetchEgo(context.Background(), func(s string) { steps = append(steps, s) })
	if err != nil {
		t.Fatal(steps, err)
	}
	defer done()
	version, _ := osexec.Command("/usr/bin/plutil", "-extract", "CFBundleShortVersionString", "raw", "-o", "-", filepath.Join(app, "Contents", "Info.plist")).Output()
	t.Logf("%v: %s, version %s", steps, filepath.Base(app), strings.TrimSpace(string(version)))
	if len(steps) != 2 {
		t.Fatal(steps)
	}
}
