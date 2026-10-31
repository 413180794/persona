package hub

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/chyroc/jarvis/server/internal/lark"
	"github.com/chyroc/jarvis/server/internal/prompts"
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
	c := prompts.LarkDocContext{Type: d.Type, Title: d.Title, URL: d.URL, CommentID: t.ID}
	if q := strings.TrimSpace(t.Quote); q != "" && !t.Whole {
		c.Quote = clipRunes(q, 3000)
	}
	loc := h.userLocation()
	for i, r := range t.Replies {
		if i > at {
			break
		}
		c.Lines = append(c.Lines, prompts.LarkLine{At: r.CreatedAt.In(loc), Who: r.Author.Name, Mine: r.Mine, Text: clipRunes(commentText(r), 2000),
			Request: i == at})
	}
	if title, md, err := h.lark.DocMarkdown(ctx, d.URL); err == nil {
		c.DocTitle, c.Markdown = title, clipRunes(md, 20000)
	} else {
		c.DocErr = clip(oneLine(err.Error()), 120)
	}
	return prompts.RenderLarkDocContext(c)
}

func larkAgentDocPrompt(owner, title, task, contextText string, resumed bool) string {
	return prompts.RenderLarkDocAgent(prompts.LarkAgent{Owner: owner, Title: title, Task: task, Context: contextText, Resumed: resumed})
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
