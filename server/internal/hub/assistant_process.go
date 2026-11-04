package hub

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/identity"
)

// The assistant runs as its own process, `personad assistant` (internal/assistant), on the hub's bus. Unless hub.json
// says assistant.external, the hub starts it beside itself, from its own binary, with a fresh bus token on its
// stdin (never on the command line; it stops when its stdin closes), and starts it again when it exits: 1 s doubling
// to a minute, from 1 s again once it ran a minute. So a hub's systemd unit, its Docker image and deploy.sh stay as they are. An assistant run
// elsewhere gets its token from `personad hub assistant-token` (docs/assistant.md「进程」).

// assistantCommand is the assistant's command line (tests run another).
var assistantCommand = func(ctx context.Context) (*exec.Cmd, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return exec.CommandContext(ctx, exe, "assistant"), nil
}

// superviseAssistant keeps the assistant's process running until ctx ends.
func (h *Hub) superviseAssistant(ctx context.Context) {
	if h.cfg.Assistant.External {
		h.logf("assistant: hub.json says it runs elsewhere (assistant.external): not starting one")
		return
	}
	token, err := h.createBusToken(ctx, assistantwire.AssistantID, "助手")
	if err != nil {
		h.logf("assistant: no bus token, not starting it: %v", err)
		return
	}
	first, most := time.Second, time.Minute
	delay := first
	for ctx.Err() == nil {
		began := time.Now()
		err := h.runAssistant(ctx, token)
		if ctx.Err() != nil {
			return
		}
		if time.Since(began) >= time.Minute {
			delay = first
		}
		h.logf("assistant: exited (%v); starting it again in %s", err, delay)
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, most)
	}
}

// runAssistant runs the assistant's process once, until it exits or ctx ends (then it is asked to stop: SIGTERM,
// killed 10 s later). Its stdin stays open while this process lives: it stops when it closes, so a hub that died
// leaves no assistant behind.
func (h *Hub) runAssistant(ctx context.Context, token string) error {
	cmd, err := assistantCommand(ctx)
	if err != nil {
		return err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	cmd.Env = identity.Environment(os.Environ(), "PERSONA_ASSISTANT_HUB="+hubLocalURL(h.cfg.Listen),
		"PERSONA_ASSISTANT_DEPLOY=child")
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 10 * time.Second
	if err := cmd.Start(); err != nil {
		return err
	}
	_, _ = io.WriteString(stdin, token+"\n")
	return cmd.Wait() // closes stdin
}

// AssistantToken is `personad hub assistant-token`: a new bus token for an assistant run elsewhere (hub.json
// assistant.external), printed once. A connection with the token it replaces is hung up.
func AssistantToken(ctx context.Context, out io.Writer) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	if cfg.DatabaseURL == "" {
		return errors.New("hub.json has no database_url")
	}
	database, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer database.Close()
	h := &Hub{cfg: cfg, store: database, logger: log.New(io.Discard, "", 0)}
	tok, err := h.createBusToken(ctx, assistantwire.AssistantID, "助手")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "%s\n\n在跑助手的机器上：PERSONA_ASSISTANT_TOKEN=<上面的 token> PERSONA_ASSISTANT_HUB=<hub 地址> personad assistant\n"+
		"（hub.json 的 assistant.external 要是 true，否则 hub 每次启动都换掉这个 token、自己起一个助手）\n", tok)
	return err
}
