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
// machine (the hub's own by default) whose CLAUDE.md tells it which of the user's data it can reach and how.

const assistantDir = ".jarvis/assistant" // relative to the machine's home

type AssistantConfig struct {
	MachineID string `json:"machine_id"`
	Launcher  string `json:"launcher"`
	// Timezone is the phone's (IANA name): routines and the snapshot read times in it.
	Timezone string `json:"timezone,omitempty"`
	// Notes, once edited in the app, replace hub.json's assistant.notes.
	Notes *string `json:"notes,omitempty"`
	// Main is the main conversation (the Jarvis tab opens it): one long-running conversation, the others are side ones.
	Main string `json:"main,omitempty"`
}

func (h *Hub) assistantConfig(ctx context.Context) AssistantConfig {
	cfg := AssistantConfig{MachineID: h.cfg.Assistant.Machine, Launcher: h.cfg.Assistant.Launcher}
	if cfg.Launcher == "" {
		cfg.Launcher = "claude"
	}
	if h.byID[cfg.MachineID] == nil {
		cfg.MachineID = ""
		for _, m := range h.machines { // the hub's own machine, else the first one
			if m.cfg.Kind == "local" {
				cfg.MachineID = m.cfg.ID
				break
			}
		}
		if cfg.MachineID == "" && len(h.machines) > 0 {
			cfg.MachineID = h.machines[0].cfg.ID
		}
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
			cfg.Timezone, cfg.Notes, cfg.Main = saved.Timezone, saved.Notes, saved.Main
		}
	}
	return cfg
}

// assistantNotes is what the assistant knows about the user: edited in the app, else hub.json's assistant.notes.
func (h *Hub) assistantNotes(ctx context.Context) string {
	if n := h.assistantConfig(ctx).Notes; n != nil {
		return strings.TrimSpace(*n)
	}
	return strings.TrimSpace(h.cfg.Assistant.Notes)
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

func (h *Hub) assistantClaudeMD(ctx context.Context) string {
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
	owner := strings.TrimSpace(h.cfg.Assistant.Owner)
	if owner == "" {
		owner = "用户"
	}
	notes := h.assistantNotes(ctx)
	if notes != "" {
		notes = "\n## 关于他（他在 App 的 设置 › 助手 › 关于我 里写的）\n\n" + notes + "\n"
	}
	extra := ""
	if dirs := h.kb.cfg.ExtraDirs; len(dirs) > 0 {
		extra = "\n- **他自己维护的笔记**：" + "`" + strings.Join(dirs, "`、`") + "`" +
			"（Markdown；" + "`kb_search`" + " 也会搜到，命中的路径是绝对路径）。读写前先看目录里的 AGENTS.md / CLAUDE.md 和对应的 skill，照它的规矩来。"
	}
	return strings.NewReplacer("{{machines}}", strings.TrimRight(machines.String(), "\n"), "{{owner}}", owner,
		"{{notes}}", notes, "{{kb_extra}}", extra).Replace(assistantPrompt)
}

const assistantPrompt = `# Jarvis 助手

你是 {{owner}} 的私人 AI 助手 **Jarvis**。他在 iPhone / Apple Watch 上的 Jarvis App 里和你对话：多半在手机上看，
回答要**先给结论**，用短段落和列表，少用宽表格和大段代码；用中文。系统提示末尾有一份「实时快照」（时间、在等他的 Agent、
飞书未读、今天接下来的会），先用它回答，需要细节再查。

## 你能访问的数据（优先用 jarvis 的 MCP 工具：快、结构化、不用找 token）

- **各台电脑上的 Agent**（herdr 里的 Claude Code / Codex）：` + "`agents_list`" + `（谁在等他、谁跑完了、最后说了什么）、
  ` + "`agent_read`" + `（读某个 Agent 最近的对话）、` + "`agent_send`" + `（替他给 Agent 发消息）、` + "`agent_start`" + `（在某台电脑的项目里开新 Agent 干开发的活）。
- **飞书**（他本人的账号，经 lark-connector）：**优先用 lark-connector skill**——` + "`ol METHOD PATH [JSON]`" + `（已在 PATH 上，别自己定义 ol 函数），
  读写消息、回复、表情、文档 / 表格、日历和会议室、妙记、待办、审批、邮件、人和组织架构，用法看 skill 的 SKILL.md 和对应的 references。
  ` + "`ol POST /context/harvest '{\"preset\":\"today\"}'`" + ` 一次拿「今天要他处理的」（@我、日程、待办、审批）。
  读直接执行；会以他的身份让别人看到的写（发 / 回复 / 撤回 / 转发消息、表情、改文档、日程、审批、给别人的待办、邮件）会先在手机上弹卡片让他确认，
  他拒绝了就别换个法子再发；只影响他自己的（标已读、完成自己的待办）直接执行。不要直接 curl lark-connector，也不要读或打印 lark-connector_AGENT_TOKEN。
  处理 ol 的输出用 ` + "`jq`" + `、` + "`head`" + `、` + "`grep`" + `（这样的读直接执行）；接 python 之类的脚本会变成整条命令都要他在手机上批准；
  看 skill 的文档用 Read 工具读 ` + "`.claude/skills/lark-connector/`" + ` 下的文件，别在 Bash 里 cat / sed。
  高频的查询也可以用 MCP：` + "`lark_chats`" + `（未读 / @我 的会话）、` + "`lark_messages`" + `（读会话）、` + "`lark_search`" + `（消息、文档、人、日程、妙记、任务、邮件）。
- **iPhone**：` + "`phone_*`" + `（通讯录、日历、提醒事项、位置、健康、照片、设备信息、剪贴板、本地通知）。写操作会在手机上让他确认。
- **邮件**：` + "`mail_*`" + `（他添加的所有邮箱：列账号、搜索、读信、发信；发信前会在手机上确认）。
- **他的电脑本身**（需要读文件、跑命令时）：
{{machines}}
  - 一次执行一条非交互命令，带超时（` + "`timeout 20 …`" + `）；Claude 会话记录在 ` + "`~/.claude/projects/<目录>/<id>.jsonl`" + `。
- **工作数据**：这台机器上装好的 skills 和 CLI（具体有哪些见文末「关于他」，没写就先 ` + "`ls ~/.claude/skills`" + ` 看看）。
- **网络**：可以联网搜索、抓网页。
- **知识库** ` + "`~/.jarvis/data`" + `（hub 自动维护，**先查这里**）：每个飞书会话 / 邮件往来的全量文字（` + "`chats/<id>/messages/日期.md`" + `）和压缩上下文
  （` + "`summary.md`" + `），每个人（` + "`people/`" + `）、每个项目（` + "`projects/<slug>/summary.md`" + `、` + "`log.md`" + ` 时间线）的档案，文档 / 会议 / 日程（` + "`docs/ meetings/ events/`" + `）；
  都带打标：importance P0–P3（重要级）、relevance 0–100（与{{owner}}的相关性）、tags。工具：` + "`kb_search`" + `（全文搜）、` + "`kb_list`" + `（按重要级排好的项目 / 会话 / 人…）、
  ` + "`kb_get`" + `（某个实体的档案 + 最近消息）、` + "`kb_update`" + `（建项目、改档案、打标、建关联、记进展）。也可以直接 ` + "`rg 关键词 ~/.jarvis/data`" + `。{{kb_extra}}

## 和他互动

- 你是无头运行的（claude -p），**AskUserQuestion 用不了**。需要他确认或做选择时用 ` + "`ask_user`" + `：手机上弹出选项卡片和推送，
  他点选后结果回到你这里。有风险的事（删除、替他给别人发消息 / 邮件、改线上配置、推代码、给 Agent 下有破坏性的指令）先用它确认。
- 他说「提醒我…」「过一小时再看看」「等 X 跑完告诉我」：用 ` + "`schedule_followup`" + ` 安排自己到时候回来接着做，做完他会收到推送；
  要立刻提醒他用 ` + "`notify_user`" + `（App 关着也能收到）。` + "`list_followups`" + ` / ` + "`cancel_followup`" + ` 管理已安排的。
- 他说「每天 / 每周几 X 点帮我…」：用 ` + "`schedule_routine`" + ` 建重复的定时任务（每次开一段新对话，他在 App 里能看、改、停），别用一串 schedule_followup 接力；
  改或停一个已有的也用它（带 id），删除用 ` + "`delete_routine`" + `。「每 30 分钟巡检一次」这种按间隔重复的，用 ` + "`schedule_followup`" + ` 带 ` + "`every`" + ` 安排一次，
  Jarvis 会自己按时重复，**不要在每一轮结尾再安排下一次**。任务用哪个模型、什么权限模式由他在 App 里定，你改不了。
- 要盯一件还在发生的事（日志里出现某行、CI 跑完、某个 URL 变了）：用 Monitor 或 Bash 的 run_in_background，然后结束这一轮；
  事件来了或命令跑完，你会被自动唤醒，再用 ` + "`notify_user`" + ` 告诉他。Monitor 最长 30 分钟，到期会通知你，还要盯就重新开；
  只是隔一阵看看的事用 ` + "`schedule_followup`" + `。jarvisd 重启（部署）会停掉这些后台任务，到时你会收到一条说明。
- 消息以「⏰」开头的是定时任务 / 你之前安排的跟进，不是他刚发的：直接把事做完，结果写成他扫一眼就懂的一屏；他会收到推送。

## 主对话和上下文

- 「Jarvis」tab 有一个常驻的**主对话**（是主对话时，实时快照开头会写明）：{{owner}}平时都在这里找你，像一个一直在线的同事，
  要接着之前聊过的事往下做；其他对话是一次性的。
- **让上下文流动**：在一段对话里得出了某个项目 / 人 / 会话的新结论（决定、进展、分工、截止时间），用 ` + "`kb_update`" + ` 写回知识库
  （项目进展写 ` + "`log`" + `，项目的代码目录写 ` + "`dirs`" + `，这样 Agents 也能对上项目）；给 Agent 派活前先 ` + "`kb_get`" + ` 相关项目，把背景带进 prompt；
  Agent 做完的结论再记回项目。
- 你的目标是**替{{owner}}上班**：主动看重点项目和会话里有什么在等{{owner}}，能自己处理的先查清楚、起草好，再请{{owner}}一键确认。

## 做事方式

- 问到「我今天 / 这周的安排」：飞书日历（` + "`ol GET /calendar/week`" + `）和 iPhone 日历（` + "`phone_calendar_events`" + `）都查，合并去重后按时间列。
- 不确定数据在哪台机器、哪个邮箱时先查再答，不要猜；查不到就直说查了哪些地方。
- 需要长时间跑的开发任务，用 ` + "`agent_start`" + ` 在对应项目开一个 Agent（告诉他在「Agents」里能看到），而不是在这里做。
- 记住他的偏好和长期有用的事实（auto memory）：他纠正你、告诉你习惯、你踩坑找到正确做法时，写进记忆；他能在 App 里看到和修改。
- 这个目录（` + "`~/.jarvis/assistant`" + `）是你的工作区，可以在这里放笔记、临时文件；本文件由 Jarvis 自动生成，会被覆盖，别改它。
{{notes}}`

type assistantView struct {
	AssistantConfig
	Cwd            string          `json:"cwd"`
	Conversations  []proto.Session `json:"conversations"`
	NotesEffective string          `json:"notes_effective"` // what the assistant is told about the user
}

func (h *Hub) assistantState(ctx context.Context) assistantView {
	cfg := h.assistantConfig(ctx)
	v := assistantView{AssistantConfig: cfg, Conversations: []proto.Session{}, NotesEffective: h.assistantNotes(ctx)}
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
			// The assistant is headless Claude Code: a launcher that runs codex would fail every message.
			if m := h.byID[cfg.MachineID]; m != nil && in.Launcher != "claude" {
				var res struct {
					Detail proto.LauncherDetail `json:"detail"`
				}
				pctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
				err := m.call(pctx, "launchers.probe", map[string]any{"name": in.Launcher}, &res)
				cancel()
				if err == nil && res.Detail.Agent != "" && res.Detail.Agent != "claude" {
					writeErr(w, http.StatusBadRequest, fmt.Errorf("%s 跑的是 %s，助手只能用启动 Claude Code 的启动器", in.Launcher, res.Detail.Agent))
					return
				}
			}
			cfg.Launcher = in.Launcher
		}
		if in.Main != "" {
			cfg.Main = in.Main
			if in.Main == "-" { // clear
				cfg.Main = ""
			}
		}
		if in.Timezone != "" {
			if _, err := time.LoadLocation(in.Timezone); err != nil {
				writeErr(w, http.StatusBadRequest, err)
				return
			}
			cfg.Timezone = in.Timezone
			h.mu.Lock()
			h.userTZ = in.Timezone
			h.snapAt = time.Time{}
			h.mu.Unlock()
		}
		if in.Notes != nil {
			cfg.Notes = in.Notes
		}
		if h.store == nil {
			writeErr(w, http.StatusServiceUnavailable, errors.New("no database"))
			return
		}
		if err := h.store.PutSetting(r.Context(), "assistant", cfg); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if in.Main != "" {
			h.keepMain(r.Context())
		}
		writeJSON(w, h.assistantState(r.Context()))
	})
	// warm: the app opened the 对话 tab; get a process ready so the first message skips claude's startup.
	api.HandleFunc("POST /api/v1/assistant/warm", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Launcher string `json:"launcher"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		go h.warmAssistant(in.Launcher)
		writeJSON(w, map[string]any{"ok": true})
	})
	api.HandleFunc("GET /api/v1/assistant/memory", func(w http.ResponseWriter, r *http.Request) {
		var res json.RawMessage
		if err := h.assistantMemory(r.Context(), map[string]any{"op": "list"}, &res); err != nil {
			callErr(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(res)
	})
	api.HandleFunc("PUT /api/v1/assistant/memory/{name}", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if err := h.assistantMemory(r.Context(), map[string]any{"op": "write", "name": r.PathValue("name"), "content": in.Content}, nil); err != nil {
			callErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	api.HandleFunc("DELETE /api/v1/assistant/memory/{name}", func(w http.ResponseWriter, r *http.Request) {
		if err := h.assistantMemory(r.Context(), map[string]any{"op": "delete", "name": r.PathValue("name")}, nil); err != nil {
			callErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	h.scheduleRoutes(api)
	api.HandleFunc("POST /api/v1/assistant/conversations", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Prompt         string        `json:"prompt"`
			Images         []proto.Image `json:"images"`
			Launcher       string        `json:"launcher"`
			PermissionMode string        `json:"permission_mode"`
			Effort         string        `json:"effort"`
			Main           bool          `json:"main"` // becomes the main conversation
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<20)).Decode(&in); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 110*time.Second)
		defer cancel()
		s, err := h.startAssistant(ctx, in.Prompt, in.Images, in.Launcher, in.PermissionMode, in.Effort)
		if err != nil {
			callErr(w, err)
			return
		}
		if in.Main && h.store != nil {
			cfg := h.assistantConfig(ctx)
			cfg.Main = s.SessionID
			if err := h.store.PutSetting(ctx, "assistant", cfg); err != nil {
				h.logf("assistant: save main: %v", err)
			}
			h.keepMain(ctx)
		}
		writeJSON(w, map[string]any{"session": s})
	})
}

func (h *Hub) startAssistant(ctx context.Context, prompt string, images []proto.Image, launcher, mode, effort string) (*proto.Session, error) {
	cfg := h.assistantConfig(ctx)
	if launcher != "" {
		cfg.Launcher = launcher // one-off choice from the app; the saved default stays
	}
	m := h.byID[cfg.MachineID]
	if m == nil {
		return nil, errors.New("assistant machine is not configured")
	}
	var prep struct {
		Cwd string `json:"cwd"`
	}
	if err := m.call(ctx, "assistant.prepare", map[string]any{"dir": assistantDir, "claude_md": h.assistantClaudeMD(ctx)}, &prep); err != nil {
		return nil, err
	}
	// Headless claude (node/assistant.go): no herdr tab, streamed tokens, any launcher per message.
	var res struct {
		SessionID string `json:"session_id"`
	}
	if err := m.call(ctx, "assistant.send", map[string]any{"cwd": prep.Cwd, "launcher": cfg.Launcher, "text": prompt, "images": images,
		"permission_mode": mode, "effort": effort}, &res); err != nil {
		return nil, err
	}
	s := proto.Session{SessionID: res.SessionID, Cwd: prep.Cwd, Status: proto.StatusWorking, Launcher: cfg.Launcher,
		PermissionMode: mode, Effort: effort, StartedByJarvis: true, UpdatedAt: time.Now().UTC()}
	s.MachineID = m.cfg.ID
	s.Assistant = true
	s.State = proto.StateWorking
	return &s, nil
}

func (h *Hub) warmAssistant(launcher string) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cfg := h.assistantConfig(ctx)
	if launcher == "" {
		launcher = cfg.Launcher
	}
	m := h.byID[cfg.MachineID]
	if m == nil {
		return
	}
	var prep struct {
		Cwd string `json:"cwd"`
	}
	if err := m.call(ctx, "assistant.prepare", map[string]any{"dir": assistantDir, "claude_md": h.assistantClaudeMD(ctx)}, &prep); err != nil {
		return
	}
	if err := m.call(ctx, "assistant.warm", map[string]any{"cwd": prep.Cwd, "launcher": launcher}, nil); err != nil {
		h.logf("warm assistant: %v", err)
	}
}

// assistantMemory runs one memory operation (node/memory.go) on the assistant's machine.
func (h *Hub) assistantMemory(ctx context.Context, op map[string]any, out any) error {
	m := h.byID[h.assistantConfig(ctx).MachineID]
	if m == nil {
		return errors.New("assistant machine is not configured")
	}
	cwd := m.assistantCwd()
	if cwd == "" {
		return errors.New("assistant machine is offline")
	}
	op["cwd"] = cwd
	return m.call(ctx, "assistant.memory", op, out)
}

// keptSessions are the sessions machine id's node lists however long they have been quiet: the assistant's main
// conversation, on the assistant's machine. Otherwise a busy machine's 80 newest transcripts crowd it out of the
// list and the Jarvis tab has no 主对话 (2026-11-01: the main one, quiet for two days, was gone).
func (h *Hub) keptSessions(ctx context.Context, machine string) []string {
	cfg := h.assistantConfig(ctx)
	if cfg.Main == "" || cfg.MachineID != machine {
		return []string{}
	}
	return []string{cfg.Main}
}

// keepMain tells the assistant's node the main conversation changed.
func (h *Hub) keepMain(ctx context.Context) {
	cfg := h.assistantConfig(ctx)
	m := h.byID[cfg.MachineID]
	if m == nil {
		return
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := m.call(cctx, "node.configure", map[string]any{"keep_sessions": h.keptSessions(cctx, m.cfg.ID)}, nil); err != nil {
		h.logf("assistant: keep main: %v", err)
	}
}
