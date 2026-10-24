package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
)

// The assistant ("对话" tab) is a Claude Code session like any other, started in a dedicated directory on one
// machine (example-machine by default) whose CLAUDE.md tells it which of the user's data it can reach and how.

const assistantDir = ".jarvis/assistant" // relative to the machine's home

type AssistantConfig struct {
	MachineID string `json:"machine_id"`
	Launcher  string `json:"launcher"`
}

func (h *Hub) assistantConfig(ctx context.Context) AssistantConfig {
	cfg := AssistantConfig{MachineID: "example-machine", Launcher: "ccc"}
	if len(h.machines) > 0 && h.byID[cfg.MachineID] == nil {
		cfg.MachineID = h.machines[0].cfg.ID
	}
	if h.store != nil {
		var saved AssistantConfig
		if ok, err := h.store.Setting(ctx, "assistant", &saved); err == nil && ok {
			if saved.MachineID != "" && h.byID[saved.MachineID] != nil {
				cfg.MachineID = saved.MachineID
			}
			if saved.Launcher != "" {
				cfg.Launcher = saved.Launcher
			}
		}
	}
	return cfg
}

// assistantCwd is the assistant directory on a machine, or "" while the machine has not reported its home.
func (m *Machine) assistantCwd() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.info.Home == "" {
		return ""
	}
	return path.Join(m.info.Home, assistantDir)
}

// isAssistant reports whether a session runs in the assistant directory (cwd may go through a symlinked home).
func isAssistant(cwd, dir string) bool {
	if dir == "" || cwd == "" {
		return false
	}
	return cwd == dir || strings.HasSuffix(cwd, "/"+assistantDir)
}

func (h *Hub) assistantClaudeMD() string {
	var machines strings.Builder
	for _, m := range h.machines {
		if m.cfg.Kind == "local" {
			fmt.Fprintf(&machines, "  - %s（`%s`）：就是你所在的这台机器，直接执行命令\n", m.cfg.Name, m.cfg.ID)
			continue
		}
		host := m.cfg.SSHHost
		if host == "" {
			host = m.cfg.ID
		}
		fmt.Fprintf(&machines, "  - %s：`ssh %s '<命令>'`\n", m.cfg.Name, host)
	}
	return strings.ReplaceAll(assistantPrompt, "{{machines}}", strings.TrimRight(machines.String(), "\n"))
}

const assistantPrompt = `# Jarvis 助手

你是 chyroc 的私人 AI 助手 **Jarvis**。他在 iPhone / Apple Watch 上的 Jarvis App 里和你对话：多半在手机上看，
回答要**先给结论**，用短段落和列表，少用宽表格和大段代码；用中文。

## 你能访问的数据

- **他的电脑**（都装了 herdr，跑着很多 Claude Code 会话）：
{{machines}}
  - 一次执行一条非交互命令；看某台机器上在跑什么：` + "`herdr agent list`" + `；Claude 会话记录在 ` + "`~/.claude/projects/<目录>/<id>.jsonl`" + `。
  - 代码一般在 ` + "`~/code`" + `；个人项目在 ` + "`~/code/chyroc`" + `。
  - **绝不访问 Mac 上的 iCloud 目录 ` + "`~/Library/Mobile Documents`" + `**（机器没解锁时会卡死）。
- **公司数据**：用这台机器上的 skills 和 CLI——飞书 ` + "`lark-*`" + `（日历、文档、消息、邮件、任务、审批、会议纪要）、
  ` + "`provider-*`" + ` / ` + "`example-cli`" + `（代码库、日志、内部搜索等）。
- **iPhone**：MCP 工具 ` + "`phone_*`" + `（通讯录、日历、提醒事项、位置、健康、照片、设备信息、剪贴板、本地通知）。
  需要 Jarvis App 在前台；写操作会在手机上让他确认。调用失败说「手机不在线」时，告诉他打开 App 再试。
- **邮件**：MCP 工具 ` + "`mail_*`" + `（他添加的所有邮箱：列账号、搜索、读信、发信；发信前会在手机上确认）。
- **网络**：可以联网搜索、抓网页。

## 做事方式

- 问到「我今天 / 这周的安排」：iPhone 日历和飞书日历都查，合并去重后按时间列。
- 不确定数据在哪台机器、哪个邮箱时先查再答，不要猜；查不到就直说查了哪些地方。
- 有风险的事（删除、给别人发消息 / 邮件、改线上配置、推代码）先用 AskUserQuestion 说清楚要做什么、让他确认。
- 需要长时间跑的开发任务，建议他在 Jarvis 的「开发」里对应项目新开会话，而不是在这里做。
- 你是无头运行的（claude -p），没有终端界面；需要他做选择时用 AskUserQuestion，会出现在他的手机上。
- 这个目录（` + "`~/.jarvis/assistant`" + `）是你的工作区，可以在这里放笔记、临时文件；本文件由 Jarvis 自动生成，会被覆盖，别改它。
`

type assistantView struct {
	AssistantConfig
	Cwd           string          `json:"cwd"`
	Conversations []proto.Session `json:"conversations"`
}

func (h *Hub) assistantState(ctx context.Context) assistantView {
	cfg := h.assistantConfig(ctx)
	v := assistantView{AssistantConfig: cfg, Conversations: []proto.Session{}}
	if m := h.byID[cfg.MachineID]; m != nil {
		v.Cwd = m.assistantCwd()
		for _, s := range h.decorated(m) {
			if s.Assistant {
				v.Conversations = append(v.Conversations, s)
			}
		}
	}
	return v
}

func (h *Hub) assistantRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/assistant", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, h.assistantState(r.Context()))
	})
	api.HandleFunc("PUT /api/v1/assistant", func(w http.ResponseWriter, r *http.Request) {
		var in AssistantConfig
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		cfg := h.assistantConfig(r.Context())
		if in.MachineID != "" {
			if h.byID[in.MachineID] == nil {
				writeErr(w, http.StatusBadRequest, errors.New("unknown machine"))
				return
			}
			cfg.MachineID = in.MachineID
		}
		if in.Launcher != "" {
			cfg.Launcher = in.Launcher
		}
		if h.store == nil {
			writeErr(w, http.StatusServiceUnavailable, errors.New("no database"))
			return
		}
		if err := h.store.PutSetting(r.Context(), "assistant", cfg); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, h.assistantState(r.Context()))
	})
	api.HandleFunc("POST /api/v1/assistant/conversations", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Prompt string        `json:"prompt"`
			Images []proto.Image `json:"images"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<20)).Decode(&in); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 110*time.Second)
		defer cancel()
		s, err := h.startAssistant(ctx, in.Prompt, in.Images)
		if err != nil {
			callErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"session": s})
	})
}

func (h *Hub) startAssistant(ctx context.Context, prompt string, images []proto.Image) (*proto.Session, error) {
	cfg := h.assistantConfig(ctx)
	m := h.byID[cfg.MachineID]
	if m == nil {
		return nil, errors.New("assistant machine is not configured")
	}
	var prep struct {
		Cwd string `json:"cwd"`
	}
	if err := m.call(ctx, "assistant.prepare", map[string]any{"dir": assistantDir, "claude_md": h.assistantClaudeMD()}, &prep); err != nil {
		return nil, err
	}
	// Headless claude (node/assistant.go): no herdr tab, streamed tokens, any launcher per message.
	var res struct {
		SessionID string `json:"session_id"`
	}
	if err := m.call(ctx, "assistant.send", map[string]any{"cwd": prep.Cwd, "launcher": cfg.Launcher, "text": prompt, "images": images}, &res); err != nil {
		return nil, err
	}
	s := proto.Session{SessionID: res.SessionID, Cwd: prep.Cwd, Status: proto.StatusWorking, Launcher: cfg.Launcher,
		StartedByJarvis: true, UpdatedAt: time.Now().UTC()}
	s.MachineID = m.cfg.ID
	s.Assistant = true
	s.State = proto.StateWorking
	return &s, nil
}
