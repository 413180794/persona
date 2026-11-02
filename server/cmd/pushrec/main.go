// Command pushrec records what an app gets from a hub over its WebSocket, for replaying it through the hub's push
// logic (internal/hub/pushreplay_test.go, docs/testing.md「推送回放」). Run it on the hub's machine:
//
//	go run ./cmd/pushrec 20m ~/push.jsonl
//
// It connects like an app with the hub token from ~/.jarvis/hub.json (never printed), registers as nothing and opens
// no chat, and writes one line per message: {"t": Unix ms, "msg": the message}. The file holds real sessions (titles,
// last lines): it is created 0600 and stays on that machine.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/coder/websocket"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: pushrec <duration> <file>")
		os.Exit(2)
	}
	dur, err := time.ParseDuration(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := record(dur, os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func record(dur time.Duration, path string) error {
	home, _ := os.UserHomeDir()
	raw, err := os.ReadFile(filepath.Join(home, ".jarvis/hub.json"))
	if err != nil {
		return err
	}
	var cfg struct{ Listen, Token string }
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return err
	}
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	w := bufio.NewWriter(out)
	defer w.Flush()
	ctx, cancel := context.WithTimeout(context.Background(), dur)
	defer cancel()
	port := cfg.Listen[strings.LastIndex(cfg.Listen, ":")+1:]
	conn, _, err := websocket.Dial(ctx, "ws://127.0.0.1:"+port+"/api/v1/ws",
		&websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + cfg.Token}}})
	if err != nil {
		return fmt.Errorf("connect to the hub on :%s failed", port) // not err: it could quote the request
	}
	conn.SetReadLimit(64 << 20)
	n, size := 0, 0
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			break
		}
		line, _ := json.Marshal(struct {
			T   int64           `json:"t"`
			Msg json.RawMessage `json:"msg"`
		}{time.Now().UnixMilli(), data})
		w.Write(line)
		w.WriteByte('\n')
		n, size = n+1, size+len(data)
	}
	fmt.Printf("recorded %d messages, %d KB\n", n, size/1024)
	return nil
}
