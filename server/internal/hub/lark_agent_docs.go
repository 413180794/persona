package hub

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/chyroc/jarvis/server/internal/lark"
)

// /agent in a document's comment (docs/lark-agent.md): no realtime event and no notification for a comment of one's
// own, so every 20 s the hub reads Feishu's 最近 list and the comments of the few documents touched in the last minutes
// (commenting opens the document, which moves it to the top). A comment reply of the user's that starts with the
// prefix is a request; the context is the document, the comment's quote and its thread; the answer is a reply in the
// same comment thread.

const (
	// Documents active this recently get their comments read.
	larkAgentDocWindow = 15 * time.Minute
	// At most this many documents per scan.
	larkAgentDocsPerScan = 5
)

// scanLarkAgentComments looks for new /agent comments in the documents touched lately.
func (h *Hub) scanLarkAgentComments(ctx context.Context) {
	cfg := h.larkAgentCfg()
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	docs, err := h.lark.RecentDocs(cctx, 20)
	if err != nil {
		h.logf("lark agent: recent docs: %v", err)
		return
	}
	me, _ := h.larkMe(cctx) // the author id backs up lark-connector's `mine`
	now := time.Now()
	read := 0
	for _, d := range docs {
		if read >= larkAgentDocsPerScan || now.Sub(d.ActivityAt) > larkAgentDocWindow || d.URL == "" {
			continue
		}
		read++
		threads, err := h.lark.Comments(cctx, d.URL, d.Type == 8)
		if err != nil {
			continue // a type without comments, no access…
		}
		for _, t := range threads {
			for i, r := range t.Replies {
				if me.UserID != "" && r.Author.ID == me.UserID {
					r.Mine = true
				}
				task, ok := larkAgentCommentTask(r, cfg.prefix())
				if !ok || r.CreatedAt.Before(h.larkAgent.since) || now.Sub(r.CreatedAt) > 10*time.Minute {
					continue
				}
				if !h.claimLarkAgent(r.ID) {
					continue
				}
				go h.runLarkAgentDoc(context.Background(), cfg, d, t, i, task)
			}
		}
	}
}

func larkAgentCommentTask(r lark.CommentReply, prefix string) (string, bool) {
	if !r.Mine {
		return "", false
	}
	return larkAgentRequest(commentText(r), prefix)
}

func (h *Hub) runLarkAgentDoc(ctx context.Context, cfg LarkAgentConfig, d lark.RecentDoc, t lark.CommentThread, at int, task string) {
	reply := t.Replies[at]
	h.larkAgent.mu.Lock()
	run := h.larkAgentRun(func(r *LarkAgentRun) bool { return r.MessageID == reply.ID })
	run.DocURL, run.CommentID, run.Chat, run.Task = d.URL, t.ID, d.Title, clip(task, 500)
	// The same comment thread asked before: go on in that conversation.
	var resume, resumeMachine string
	if prev := h.larkAgentRun(func(r *LarkAgentRun) bool { return r != run && r.CommentID == t.ID && r.SessionID != "" }); prev != nil {
		resume, resumeMachine = prev.SessionID, prev.MachineID
	}
	h.larkAgent.mu.Unlock()
	h.saveLarkAgentRuns(ctx)

	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	contextText := h.larkAgentDocContext(cctx, d, t, at)
	prompt := larkAgentDocPrompt(h.ownerName(), d.Title, task, contextText, resume != "")
	machineID, sessionID, err := h.startLarkAgent(cctx, cfg, resumeMachine, resume, prompt)
	cancel()

	h.larkAgent.mu.Lock()
	if err != nil {
		run.State, run.Error = "failed", clip(oneLine(err.Error()), 300)
	} else {
		run.MachineID, run.SessionID = machineID, sessionID
	}
	h.larkAgent.mu.Unlock()
	h.saveLarkAgentRuns(ctx)
	if err != nil {
		h.logf("lark agent: start for comment %s: %v", reply.ID, err)
		_ = h.larkAgentPost(ctx, run, "这次没能开始处理（"+clip(oneLine(err.Error()), 80)+"）。")
	}
}

// larkAgentDocContext: the document (as Markdown), what the comment is on (its quote), and the thread so far.
func (h *Hub) larkAgentDocContext(ctx context.Context, d lark.RecentDoc, t lark.CommentThread, at int) string {
	var b strings.Builder
	kind := map[int]string{22: "文档", 2: "文档", 3: "电子表格", 8: "多维表格", 16: "知识库文档"}[d.Type]
	if kind == "" {
		kind = "文档"
	}
	fmt.Fprintf(&b, "<jarvis-context kind=\"context\" title=\"%s\">\n位置：%s「%s」里的一条评论（%s）；评论 id %s。\n</jarvis-context>\n",
		xmlAttr("请求的位置"), kind, d.Title, d.URL, t.ID)
	if q := strings.TrimSpace(t.Quote); q != "" && !t.Whole {
		fmt.Fprintf(&b, "<jarvis-context kind=\"context\" title=\"%s\">\n%s\n</jarvis-context>\n", xmlAttr("评论划选的原文"), clipRunes(q, 3000))
	}
	var lines []string
	for i, r := range t.Replies {
		if i > at {
			break
		}
		who := r.Author.Name
		if r.Mine {
			who += "（我）"
		}
		mark := ""
		if i == at {
			mark = "  ← 请求"
		}
		lines = append(lines, fmt.Sprintf("%s %s：%s%s", r.CreatedAt.Local().Format("01-02 15:04"), who, clipRunes(commentText(r), 2000), mark))
	}
	fmt.Fprintf(&b, "<jarvis-context kind=\"context\" title=\"%s\">\n%s\n</jarvis-context>\n", xmlAttr("这条评论里的讨论"), strings.Join(lines, "\n"))
	if title, md, err := h.lark.DocMarkdown(ctx, d.URL); err == nil {
		fmt.Fprintf(&b, "<jarvis-context kind=\"doc\" title=\"%s\" url=\"%s\">\n%s\n</jarvis-context>\n", xmlAttr("文档 · "+title), xmlAttr(d.URL), clipRunes(md, 20000))
	} else {
		fmt.Fprintf(&b, "（文档正文读不到：%s；需要的话用 lark-connector 的 documents/read 自己读，表格 / 多维表格按它们的接口读。）\n", clip(oneLine(err.Error()), 120))
	}
	return b.String()
}

func larkAgentDocPrompt(owner, title, task, contextText string, resumed bool) string {
	if owner == "" {
		owner = "我"
	}
	var b strings.Builder
	if resumed {
		fmt.Fprintf(&b, "%s在同一条评论里又写了一条 /agent 请求（下面是最新的上下文）。\n\n", owner)
	} else {
		fmt.Fprintf(&b, "%s在飞书文档「%s」的评论里用 /agent 叫你。你只知道下面给出的上下文；不够就自己查（读文档、lark_*、kb_*、看各台电脑上的 Agent）。\n\n", owner, title)
	}
	if task == "" {
		task = "（没写具体要求：看评论划选的原文和讨论，判断他想让你做什么，比如解释、核对、给修改建议。）"
	}
	fmt.Fprintf(&b, "## 请求\n\n%s\n\n## 上下文\n\n%s\n", task, contextText)
	b.WriteString(`
## 怎么做

- 能做的直接做掉：查资料、读代码、看日志、跑命令、开 Agent 干活都可以。**不要改这份文档的正文**，要改就在回复里给出建议的写法。
- 做完用 lark_agent_reply 回复一次（只调一次，在最后）：hub 会以` + owner + `的身份回复在这条评论里，末尾自动加「🤖」。
  评论是纯文本：不用 Markdown 符号，分点用「1. 2.」或换行；直接给结论，短一些。
- **不替` + owner + `做承诺**：回复是以他的名义发的。只写已经做完的事；没做的写成建议，排期、答应别人的事一律只给建议。
- 别用别的方式往飞书发消息或评论，别 ask_user / notify_user 打扰` + owner + `：这里的回复就是交付。
- 有新结论用 kb_update 写回知识库。
`)
	return b.String()
}

var (
	mdEmphasis = regexp.MustCompile(`\*\*([^*]+)\*\*|__([^_]+)__`)
	mdHeading  = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	mdLink     = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	mdBullet   = regexp.MustCompile(`(?m)^(\s*)[-*]\s+`)
	mdQuote    = regexp.MustCompile(`(?m)^>\s?`)
)

// plainText drops the Markdown marks an agent writes out of habit, for a plain-text doc comment.
func plainText(md string) string {
	s := mdEmphasis.ReplaceAllString(md, "$1$2")
	s = mdHeading.ReplaceAllString(s, "")
	s = mdLink.ReplaceAllString(s, "$1（$2）")
	s = mdBullet.ReplaceAllString(s, "$1· ")
	s = mdQuote.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "`", "")
	return strings.TrimSpace(s)
}
