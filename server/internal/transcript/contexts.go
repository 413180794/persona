package transcript

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// A message can carry <jarvis-context kind="…" title="…" url="…"> blocks: context picked on the phone, or everything
// a Feishu /agent request hands its agent (prompts/lark). The apps show only the text outside the blocks as what the
// user wrote and fold the blocks into one row (docs/lark-agent.md「在 App 里怎么显示」).

// contextBlockRe is one attributed block (the bare <jarvis-context> snapshot, ContextRe, is dropped before this).
var contextBlockRe = regexp.MustCompile(`(?s)(<jarvis-context\b[^>]*>)(.*?)(</jarvis-context>)`)

// legacyLarkAgentRe is a Feishu /agent prompt as hub wrote it before the instructions moved into a block: an intro line,
// then ## 请求 (what was asked), ## 上下文 (blocks, plus a loose line or two), ## 怎么做 (the rules).
var legacyLarkAgentRe = regexp.MustCompile(`(?s)^([^\n]*(?:用 /agent 叫你|又(?:发|写)了一条 /agent 请求)[^\n]*)\n+## 请求\n+(.*?)\n+## 上下文\n+(.*)\n+## 怎么做\n+(.*?)\s*$`)

// legacyLarkAgent rewrites an old Feishu /agent prompt into today's shape, so conversations started before the change
// show the same way: the instructions and context in blocks, the request outside them.
func legacyLarkAgent(s string) string {
	m := legacyLarkAgentRe.FindStringSubmatch(s)
	if m == nil {
		return s
	}
	intro, task, ctx, rules := m[1], strings.TrimSpace(m[2]), m[3], m[4]
	blocks := contextBlockRe.FindAllString(ctx, -1)
	loose := strings.TrimSpace(contextBlockRe.ReplaceAllString(ctx, ""))
	var b strings.Builder
	b.WriteString(`<jarvis-context kind="instructions" title="给 Agent 的说明">` + "\n" + intro + "\n")
	if strings.HasPrefix(task, "（") { // no request written: the note in its place is for the agent
		b.WriteString("\n" + task + "\n")
		task = "/agent"
	}
	b.WriteString("\n## 怎么做\n\n" + rules + "\n")
	if loose != "" {
		b.WriteString("\n" + loose + "\n")
	}
	b.WriteString("</jarvis-context>\n")
	for _, bl := range blocks {
		b.WriteString(bl + "\n")
	}
	b.WriteString(task)
	return b.String()
}

// lightContexts fits a message with context blocks into limit bytes by cutting the blocks' bodies, longest first,
// so what the user wrote and every block's title stay whole (a plain cut could end inside a block). ok is false when
// even that does not fit, or there is nothing to cut.
func lightContexts(s string, limit int) (string, bool) {
	locs := contextBlockRe.FindAllStringSubmatchIndex(s, -1)
	if len(locs) == 0 {
		return "", false
	}
	fixed := len(s)
	bodies := make([]int, len(locs)) // body lengths
	for i, l := range locs {
		bodies[i] = l[5] - l[4]
		fixed -= bodies[i]
	}
	const note = "\n…（已截断，共 %d 字节）"
	budget := limit - fixed - len(locs)*(len(note)+8)
	if budget < len(locs)*64 {
		return "", false
	}
	// Water-fill: short bodies stay whole, the long ones share what is left equally.
	order := make([]int, len(locs))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool { return bodies[order[a]] < bodies[order[b]] })
	keep := make([]int, len(locs))
	left := budget
	for n, i := range order {
		share := left / (len(order) - n)
		keep[i] = min(bodies[i], share)
		left -= keep[i]
	}
	var b strings.Builder
	prev := 0
	for i, l := range locs {
		b.WriteString(s[prev:l[4]])
		body := s[l[4]:l[5]]
		if keep[i] < len(body) {
			end := keep[i]
			for end > 0 && (body[end]&0xC0) == 0x80 {
				end--
			}
			if nl := strings.LastIndexByte(body[:end], '\n'); nl > end-256 && nl > 0 {
				end = nl
			}
			body = body[:end] + fmt.Sprintf(note, len(body)) + "\n"
		}
		b.WriteString(body)
		prev = l[5]
	}
	b.WriteString(s[prev:])
	if b.Len() > limit {
		return "", false
	}
	return b.String(), true
}

// injectedBlockRes are the blocks Persona and Claude Code put into a user message around what the user wrote: the
// <jarvis-context> snapshot and attributed blocks, the <persona-view-errors> feedback, <system-reminder>s.
var injectedBlockRes = []*regexp.Regexp{
	regexp.MustCompile(`(?s)<jarvis-context\b[^>]*>.*?</jarvis-context>`),
	regexp.MustCompile(`(?s)<persona-view-errors\b[^>]*>.*?</persona-view-errors>`),
	regexp.MustCompile(`(?s)<system-reminder\b[^>]*>.*?</system-reminder>`),
}

// injectedOpenRe is one of those blocks left open: a message clipped inside it (Meta.FirstUser is) ends there.
var injectedOpenRe = regexp.MustCompile(`<(?:jarvis-context|persona-view-errors|system-reminder)\b`)

// StripInjected is what the user wrote in a message, for a line that shows it (a title, a notification): without the
// injected blocks, a block cut off by a clip, and the paste wrappers (what was pasted stays). "" when nothing is left.
func StripInjected(s string) string {
	if !strings.Contains(s, "<") {
		return strings.TrimSpace(s)
	}
	for _, re := range injectedBlockRes {
		s = re.ReplaceAllString(s, " ")
	}
	if loc := injectedOpenRe.FindStringIndex(s); loc != nil {
		s = s[:loc[0]]
	}
	return strings.TrimSpace(pastedRe.ReplaceAllString(s, ""))
}
