package kb

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Model is the small model that compresses and tags (the hub's ai.Client).
type Model interface {
	JSONMax(ctx context.Context, system, user string, maxTokens int, out any) error
}

// When a chat is due for compression: enough new messages, or some new ones and a quiet while.
const (
	compressBatch = 50
	compressQuiet = 10 * time.Minute
	compressInput = 24000 // characters of new messages per call
)

var importanceRe = regexp.MustCompile(`^P[0-3]$`)

type chatDigest struct {
	Summary    string   `json:"summary"`
	OneLine    string   `json:"one_line"`
	Importance string   `json:"importance"`
	Relevance  int      `json:"relevance"`
	Tags       []string `json:"tags"`
	Projects   []struct {
		Name    string   `json:"name"`
		Kind    string   `json:"kind"`
		Aliases []string `json:"aliases"`
		Update  string   `json:"update"`
		MyRole  string   `json:"my_role"`
	} `json:"projects"`
	People []struct {
		Name string `json:"name"`
		Note string `json:"note"`
	} `json:"people"`
}

const compressPrompt = `你在维护 {{owner}} 的工作知识库。输入是一个会话（飞书私聊 / 群 / 邮件往来）已有的压缩上下文，加上之后的新消息。
「我」就是 {{owner}}。输出这个会话新的压缩上下文和打标，全部用中文。

JSON 字段：
- summary：Markdown，800 字以内，覆盖已有上下文和新消息，旧的不重要的细节可以删。固定小节：
  「## 这是什么」（会话的用途、谁在里面）、「## 进展」（按时间的要点，最近的在后，带日期）、「## 待办和未决」（谁要做什么、是否在等我）、
  「## 关于我」（和我相关的事、别人对我的期待）。没有的小节写「无」。
- one_line：一句话（30 字内）说这个会话现在在干什么。
- importance：P0（关键：老板 / 核心项目 / 线上事故 / 截止临近）、P1（重要）、P2（一般）、P3（可忽略：通知、闲聊、广告）。
- relevance：0–100，与我相关性。直接找我、我负责、在等我 → 70 以上；我参与但不是主角 → 40–70；只是旁观 → 40 以下。
- 通知类（输入里标了「自动发出」的邮件，或者内容是系统通知、推广营销，哪怕署了人名）：收据、账单、续费 / 订阅、订单、推广营销、newsletter、欢迎信、
  「协议已接受 / 资料已验证 / 已加入某计划」这类告知、测试邀请，一律 importance P3、relevance 20 以下，哪怕提到的是我的账号或产品。
  只有要我在期限内去做点什么、不做会有后果的才按实际打高：存储超限要删文件、账号受限、扣款失败、不是我触发的安全提醒、
  审核被拒要改、银行 / 平台要我补材料或约时间。
- tags：2–6 个短标签，不预设；优先复用「已有标签」里意思相同的。
- projects：会话里真人在推进的项目（需求、版本、活动、团队方向）。系统通知、营销、订阅、账单、自动提醒里提到的产品或服务**不是项目**，不要列。name 优先用「已知项目」里的原名；kind 是 short（有交付的短期项目）或 long
  （团队使命、长期方向）；aliases 是别称；update 一句话说这批新消息里这个项目有什么进展（没有就不要列这个项目）；my_role 是我在里面的角色（不知道就空）。
- people：新消息里值得记住的人，name 用消息里的名字，note 一句话说他是谁、和我什么关系（不知道就不列）。
不知道的不要编。`

// DueForCompression lists chats with news to fold into their summary.
func (e *Engine) DueForCompression(now time.Time) []*Chat {
	var out []*Chat
	for _, c := range e.Store.Chats() {
		if c.Pending == 0 {
			continue
		}
		if c.Pending >= compressBatch || now.Sub(c.LastAt) >= compressQuiet || c.CompressedAt.IsZero() && now.Sub(c.Created) >= compressQuiet {
			out = append(out, c)
		}
	}
	// The chats that matter most first; then the busiest.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Relevance != out[j].Relevance {
			return out[i].Relevance > out[j].Relevance
		}
		return out[i].Pending > out[j].Pending
	})
	return out
}

// Compress folds a chat's new messages into its summary and tags, then evolves the projects and people it mentions.
func (e *Engine) Compress(ctx context.Context, c *Chat) error {
	if e.Model == nil {
		return nil
	}
	from := c.CoveredUntil
	if from.IsZero() {
		from = c.Created.AddDate(0, 0, -30)
	}
	news := e.Store.Since(c.ID, from, compressInput)
	if strings.TrimSpace(news) == "" {
		c.Pending = 0
		return e.Store.PutChat(c)
	}
	kind := map[string]string{"dm": "私聊", "group": "群聊", "bot": "机器人 / AI", "mail": "邮件往来", "moment": "removed帖子和评论（公司内公开）"}[c.Kind]
	if c.Kind == "mail" && !HasPeople(c) {
		kind += "，自动发出（通知 / 营销地址，不是真人写的）"
	}
	var in strings.Builder
	fmt.Fprintf(&in, "会话：%s（%s）\n\n已知项目：%s\n已有标签：%s\n\n# 已有的压缩上下文\n%s\n\n# 新消息\n%s",
		c.Name, kind, e.projectNames(), strings.Join(e.TopTags(40), "、"), orNone(e.Store.Summary(KindChat, c.ID)), news)

	var d chatDigest
	cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	if err := e.Model.JSONMax(cctx, strings.ReplaceAll(compressPrompt, "{{owner}}", e.Owner), in.String(), 4000, &d); err != nil {
		return err
	}
	now := time.Now().UTC()
	if strings.TrimSpace(d.Summary) != "" {
		if err := e.Store.PutSummary(KindChat, c.ID, "# "+c.Name+"\n\n"+d.Summary); err != nil {
			return err
		}
	}
	c.OneLine = oneLine(d.OneLine)
	applyTags(&c.Tagging, d.Importance, d.Relevance, d.Tags, now)
	if HasPeople(c) {
		for _, p := range d.Projects {
			slug := e.evolveProject(p.Name, p.Kind, p.Aliases, p.MyRole, c, p.Update, now)
			c.Projects = addUnique(c.Projects, slug)
		}
	}
	for _, p := range d.People {
		e.notePersonNote(c, p.Name, p.Note)
	}
	c.Pending = 0
	c.CompressedAt = now
	c.CoveredUntil = c.LastAt.Add(time.Minute)
	c.TagRev = tagRev
	if err := e.Store.PutChat(c); err != nil {
		return err
	}
	e.index(KindChat, c.ID, c.Name, c.Tagging, c.OneLine, c.LastAt)
	// A DM is mostly about the other person: their profile follows it.
	if c.Kind == "dm" || c.Kind == "mail" {
		for _, pid := range c.People {
			if p, ok := e.Store.Person(pid); ok && !p.Locked {
				p.Importance, p.Relevance = c.Importance, c.Relevance
				p.Tags = c.Tags
				_ = e.Store.PutPerson(p)
			}
		}
	}
	for _, pid := range c.People {
		e.WritePersonSummary(pid)
	}
	return nil
}

// automatedSender matches mail addresses of notifications and newsletters (only the inbox is read, so a reply of
// the owner's rarely shows up: the sender is what tells).
var automatedSender = regexp.MustCompile(`(?i)(^|[-_.@])(no[-_]?reply|do[-_]?not[-_]?reply|notifications?|notify|news(letter)?|marketing|mailer|mailer-daemon|postmaster|bounces?|info|support|service|billing|account|alerts?|updates?|hello|team|receipts?|orders?|invoices?|payments?|contact|sales|promo(tions)?|offers?|rewards?|digest|submissions?|feedback|welcome)([-_.@]|$)`)

// automatedLocal: a local part ending in one of those words ("applesupport", "teamnotify").
var automatedLocal = regexp.MustCompile(`(?i)(support|service|notify|notifications?|alerts?|news|updates?|receipts?|billing)$`)

// bulkDomain: the subdomains bulk mail goes out from (email.apple.com, em-s.dropbox.com, txn.dropbox.com, mg.…).
var bulkDomain = regexp.MustCompile(`(?i)^(e|em|em-[a-z]+|email|emails|emessaging|message|messages|mail|mailer|mailing|news|newsletter|txn|transactional|notify|notifications|mg|send|sendgrid|bounce|marketing)[.]`)

// newsletterHost: platforms every sender on is a newsletter.
var newsletterHost = regexp.MustCompile(`(?i)(^|[.])(substack\.com|beehiiv\.com|mailchimp(app)?\.com|convertkit\.com|buttondown\.email|ghost\.io|medium\.com)$`)

// automatedAddress reports whether a mail address looks like a system or bulk sender rather than a person.
func automatedAddress(addr string) bool {
	if automatedSender.MatchString(addr) {
		return true
	}
	local, domain, ok := strings.Cut(addr, "@")
	return ok && (automatedLocal.MatchString(local) || bulkDomain.MatchString(domain) || newsletterHost.MatchString(domain))
}

// HasPeople reports whether real people talk in a chat: chats with bots and automated mail (notifications,
// newsletters) do not make projects.
func HasPeople(c *Chat) bool {
	switch c.Kind {
	case "bot":
		return false
	case "mail":
		return c.Mine || !automatedAddress(c.SourceID[strings.Index(c.SourceID, "-")+1:])
	}
	return true
}

// IgnoreNoiseProjects marks projects that only come from chats without people as ignored (the AI once made a
// project of every app named in store emails). Hand-tagged ones stay. Returns how many changed.
func (e *Engine) IgnoreNoiseProjects() int {
	n := 0
	for _, p := range e.Store.Projects() {
		if p.Locked || p.Status == "ignored" || len(p.Chats) == 0 {
			continue
		}
		people := false
		for _, cid := range p.Chats {
			if c, ok := e.Store.Chat(cid); !ok || HasPeople(c) {
				people = true
				break
			}
		}
		if !people {
			p.Status = "ignored"
			p.Dirty = false
			if e.Store.PutProject(p) == nil {
				e.index(KindProject, p.Slug, p.Name, p.Tagging, p.OneLine, p.Updated)
				n++
			}
		}
	}
	return n
}

// tagRev is the version of the tagging rules; chats tagged under an older one can be tagged again (RetagMail).
// 1: notifications (receipts, renewals, promotions, invitations) are P3 and at most 20 unless they need me to act.
const tagRev = 1

// RetagMail queues mail the owner never replied to, tagged under older rules, for one more compression, so the rules
// reach what is already there (receipts and promotions had come out at relevance 60–75, some from addresses that
// passed for people). Hand-tagged ones stay. Returns how many were queued.
func (e *Engine) RetagMail() int {
	n := 0
	for _, c := range e.Store.Chats() {
		if c.Kind != "mail" || c.Mine || c.Locked || c.TagRev >= tagRev || c.CompressedAt.IsZero() {
			continue
		}
		c.CoveredUntil = time.Time{} // read its messages again; the old summary goes in as context
		c.Pending = max(c.Pending, 1)
		if e.Store.PutChat(c) == nil {
			n++
		}
	}
	return n
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "（还没有）"
	}
	return s
}

func applyTags(t *Tagging, importance string, relevance int, tags []string, now time.Time) {
	if !t.Locked {
		if importanceRe.MatchString(importance) {
			t.Importance = importance
		}
		if relevance >= 0 && relevance <= 100 {
			t.Relevance = relevance
		}
	}
	var clean []string
	for _, tag := range tags {
		tag = strings.TrimSpace(strings.TrimPrefix(tag, "#"))
		if tag != "" && len([]rune(tag)) <= 16 {
			clean = addUnique(clean, tag)
		}
	}
	if len(clean) > 0 {
		t.Tags = clean
	}
	t.TaggedAt = now
}

// --- projects ---

var slugRe = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// Slug is the directory name of a project name ("Jarvis 2.0 发布" → "jarvis-2-0-发布").
func Slug(name string) string {
	s := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if r := []rune(s); len(r) > 40 {
		s = string(r[:40])
	}
	return s
}

func normName(s string) string { return strings.ReplaceAll(Slug(s), "-", "") }

// FindProject matches a name against project names and aliases.
func (e *Engine) FindProject(name string) *Project {
	n := normName(name)
	if n == "" {
		return nil
	}
	for _, p := range e.Store.Projects() {
		if normName(p.Name) == n || p.Slug == Slug(name) {
			return p
		}
		for _, a := range p.Aliases {
			if normName(a) == n {
				return p
			}
		}
	}
	return nil
}

func (e *Engine) projectNames() string {
	var names []string
	for _, p := range e.Store.Projects() {
		if p.Status == "done" || p.Status == "ignored" {
			continue
		}
		n := p.Name
		if len(p.Aliases) > 0 {
			n += "（" + strings.Join(p.Aliases, "、") + "）"
		}
		names = append(names, n)
	}
	if len(names) == 0 {
		return "（还没有）"
	}
	return strings.Join(names, "；")
}

// evolveProject merges a mention into its project (creating it), links the chat and its people, and logs the update.
func (e *Engine) evolveProject(name, kind string, aliases []string, myRole string, c *Chat, update string, now time.Time) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	p := e.FindProject(name)
	if p == nil {
		for _, a := range aliases {
			if p = e.FindProject(a); p != nil {
				break
			}
		}
	}
	if p != nil && p.Status == "ignored" {
		return ""
	}
	if p == nil {
		slug := Slug(name)
		if slug == "" {
			return ""
		}
		p = &Project{Slug: slug, Name: name, Kind: "short", Status: "active", Created: now}
	}
	if kind == "short" || kind == "long" {
		if p.Kind == "" || p.Kind == "short" {
			p.Kind = kind // long wins: a mission does not become a feature
		}
	}
	for _, a := range aliases {
		if normName(a) != normName(p.Name) {
			p.Aliases = addUnique(p.Aliases, strings.TrimSpace(a))
		}
	}
	if myRole != "" {
		p.MyRole = oneLine(myRole)
	}
	p.Chats = addUnique(p.Chats, c.ID)
	p.People = addUnique(p.People, c.People...)
	if strings.TrimSpace(update) != "" {
		_ = e.Store.AppendLog(p.Slug, now, c.Name, update)
		p.Dirty = true
		if p.Status == "done" {
			p.Status = "active"
		}
	}
	_ = e.Store.PutProject(p)
	for _, pid := range c.People {
		if per, ok := e.Store.Person(pid); ok {
			n := len(per.Projects)
			if per.Projects = addUnique(per.Projects, p.Slug); len(per.Projects) != n {
				_ = e.Store.PutPerson(per)
			}
		}
	}
	return p.Slug
}

type projectDigest struct {
	Summary    string   `json:"summary"`
	OneLine    string   `json:"one_line"`
	Goal       string   `json:"goal"`
	Kind       string   `json:"kind"`
	Status     string   `json:"status"`
	Importance string   `json:"importance"`
	Relevance  int      `json:"relevance"`
	Tags       []string `json:"tags"`
}

const projectPrompt = `你在维护 {{owner}} 的项目档案。「我」就是 {{owner}}。输入是一个项目已有的档案、最近的时间线、相关会话的压缩上下文。
重写这个项目的档案，全部用中文。JSON 字段：
- summary：Markdown，1000 字以内。固定小节：「## 目标」「## 现状」（最新进展，带日期）「## 下一步」（谁做什么、什么时候）
  「## 风险和阻塞」「## 我的角色」「## 相关的人」（名字 + 一句话角色）。没有的写「无」。
- one_line：一句话（30 字内）说项目现在到哪了。
- goal：一句话目标。kind：short（有交付的短期项目）或 long（团队使命 / 长期方向）。status：active / paused / done。
- importance：P0–P3（对我的重要级）；relevance：0–100（与我相关性）；tags：2–6 个短标签，优先复用已有标签。
不知道的不要编。`

// RefreshProjects rewrites the summaries of projects with news, at most max per run.
func (e *Engine) RefreshProjects(ctx context.Context, max int) int {
	if e.Model == nil {
		return 0
	}
	var dirty []*Project
	for _, p := range e.Store.Projects() {
		if p.Dirty && p.Status != "ignored" {
			dirty = append(dirty, p)
		}
	}
	sort.Slice(dirty, func(i, j int) bool { return dirty[i].Relevance > dirty[j].Relevance })
	n := 0
	for _, p := range dirty {
		if n >= max || ctx.Err() != nil {
			break
		}
		if err := e.refreshProject(ctx, p); err != nil {
			e.logf("project %s: %v", p.Slug, err)
			continue
		}
		n++
	}
	return n
}

func (e *Engine) refreshProject(ctx context.Context, p *Project) error {
	var in strings.Builder
	fmt.Fprintf(&in, "项目：%s（%s）\n别称：%s\n已有标签：%s\n\n# 已有档案\n%s\n\n# 最近的时间线\n%s\n\n# 相关会话\n",
		p.Name, p.Kind, strings.Join(p.Aliases, "、"), strings.Join(e.TopTags(40), "、"),
		orNone(e.Store.Summary(KindProject, p.Slug)), strings.Join(e.Store.Log(p.Slug, 40), "\n"))
	for i, cid := range p.Chats {
		if i >= 8 {
			break
		}
		sum := e.Store.Summary(KindChat, cid)
		if r := []rune(sum); len(r) > 1500 {
			sum = string(r[:1500]) + "…"
		}
		fmt.Fprintf(&in, "\n## %s\n%s\n", cid, sum)
	}
	var d projectDigest
	cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	if err := e.Model.JSONMax(cctx, strings.ReplaceAll(projectPrompt, "{{owner}}", e.Owner), in.String(), 4000, &d); err != nil {
		return err
	}
	now := time.Now().UTC()
	if strings.TrimSpace(d.Summary) != "" {
		if err := e.Store.PutSummary(KindProject, p.Slug, "# "+p.Name+"\n\n"+d.Summary); err != nil {
			return err
		}
	}
	p.OneLine = oneLine(d.OneLine)
	if d.Goal != "" {
		p.Goal = oneLine(d.Goal)
	}
	if d.Kind == "short" || d.Kind == "long" {
		p.Kind = d.Kind
	}
	if (d.Status == "active" || d.Status == "paused" || d.Status == "done") && p.Status != "ignored" {
		p.Status = d.Status
	}
	applyTags(&p.Tagging, d.Importance, d.Relevance, d.Tags, now)
	p.Dirty = false
	if err := e.Store.PutProject(p); err != nil {
		return err
	}
	e.index(KindProject, p.Slug, p.Name, p.Tagging, p.OneLine, p.Updated)
	return nil
}

// --- people ---

func (e *Engine) notePersonNote(c *Chat, name, note string) {
	name, note = strings.TrimSpace(name), strings.TrimSpace(note)
	if name == "" || note == "" {
		return
	}
	for _, pid := range c.People {
		if p, ok := e.Store.Person(pid); ok && p.Name == name {
			p.Note = oneLine(note)
			_ = e.Store.PutPerson(p)
			e.index(KindPerson, p.ID, p.Name, p.Tagging, p.Note, p.LastSeen)
			return
		}
	}
}

// WritePersonSummary renders people/<id>/summary.md from what is known (no model call): who, where, which projects,
// and the DM's context.
func (e *Engine) WritePersonSummary(pid string) {
	p, ok := e.Store.Person(pid)
	if !ok {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", p.Name)
	if p.Note != "" {
		fmt.Fprintf(&b, "%s\n\n", p.Note)
	}
	if p.Importance != "" {
		fmt.Fprintf(&b, "重要级 %s · 与我相关性 %d · %s\n\n", p.Importance, p.Relevance, strings.Join(p.Tags, " "))
	}
	if len(p.Projects) > 0 {
		b.WriteString("## 项目\n")
		for _, slug := range p.Projects {
			if pr, ok := e.Store.Project(slug); ok {
				fmt.Fprintf(&b, "- %s（projects/%s）%s\n", pr.Name, pr.Slug, prefixed(" — ", pr.OneLine))
			}
		}
		b.WriteString("\n")
	}
	if len(p.Chats) > 0 {
		b.WriteString("## 会话\n")
		for _, cid := range p.Chats {
			if c, ok := e.Store.Chat(cid); ok {
				fmt.Fprintf(&b, "- %s（chats/%s，%s）%s\n", c.Name, c.ID, c.Kind, prefixed(" — ", c.OneLine))
			}
		}
		b.WriteString("\n")
	}
	for _, cid := range p.Chats {
		if c, ok := e.Store.Chat(cid); ok && (c.Kind == "dm" || c.Kind == "mail") {
			if sum := strings.TrimSpace(e.Store.Summary(KindChat, cid)); sum != "" {
				fmt.Fprintf(&b, "## 和我的私聊\n%s\n", strings.TrimPrefix(sum, "# "+c.Name+"\n\n"))
			}
		}
	}
	_ = e.Store.PutSummary(KindPerson, pid, b.String())
}

func prefixed(p, s string) string {
	if s == "" {
		return ""
	}
	return p + s
}

// TopTags returns the n most used tags across the knowledge base.
func (e *Engine) TopTags(n int) []string {
	count := map[string]int{}
	add := func(t []string) {
		for _, x := range t {
			count[x]++
		}
	}
	for _, c := range e.Store.Chats() {
		add(c.Tags)
	}
	for _, p := range e.Store.Projects() {
		add(p.Tags)
	}
	tags := make([]string, 0, len(count))
	for t := range count {
		tags = append(tags, t)
	}
	sort.Slice(tags, func(i, j int) bool {
		if count[tags[i]] != count[tags[j]] {
			return count[tags[i]] > count[tags[j]]
		}
		return tags[i] < tags[j]
	})
	if len(tags) > n {
		tags = tags[:n]
	}
	return tags
}
