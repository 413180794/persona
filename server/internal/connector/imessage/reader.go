// Package imessage reads the Messages app's data on a Mac (docs/plan-connectors.md「五、iMessage 连接器」):
// ~/Library/Messages/chat.db and the address book for names, read-only, through macOS's own /usr/bin/sqlite3
// (jarvisd is built without cgo). It runs in the node of the Mac the hub was told to read (imessage.machine): the
// files are protected by macOS (Full Disk Access), and nothing here writes to them, marks anything read or sends.
//
// Conversations are what the user sees in Messages: a person's chats over iMessage, SMS and RCS are one, a group is
// its own. Their ids are hashes (imessage-<12 hex>): no phone number in an id, a URL or a log line.
package imessage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/connector"
)

// ID is the connector's id: the key in hub.json, the RPCs' "id".
const ID = "imessage"

// Reader reads one home's Messages data.
type Reader struct {
	Home string
	// SQLite is the sqlite3 to run; macOS's own when empty.
	SQLite string
	// Via is how this node runs, for the permission guide: "launchd" (a resident node: Full Disk Access goes to
	// jarvisd) or "ssh" (it goes to sshd-keygen-wrapper, through 远程登录's switch).
	Via string
	// Executable is this jarvisd's path, for the launchd guide.
	Executable string

	mu       sync.Mutex
	contacts *contacts
	loaded   time.Time
}

// Status is whether this Mac's messages can be read, and if not, how to fix it.
type Status struct {
	State string   `json:"status"` // ok | not_configured | no_permission | error
	Error string   `json:"error,omitempty"`
	Guide []string `json:"guide,omitempty"` // steps on this Mac, when State is no_permission
	Via   string   `json:"via,omitempty"`
}

// Person is someone in a conversation: their number or address, and their name in this Mac's contacts.
type Person struct {
	Handle string `json:"handle"`
	Name   string `json:"name,omitempty"`
}

// Chat is a conversation as the list shows it.
type Chat struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Group    bool      `json:"group,omitempty"`
	Members  []Person  `json:"members"` // the others; for a one-to-one chat, the one
	Services []string  `json:"services"`
	Last     time.Time `json:"last"`
	Preview  string    `json:"preview,omitempty"`
	FromMe   bool      `json:"from_me,omitempty"` // the last message is the user's
	Unread   int       `json:"unread,omitempty"`
}

// Message is one message, or a line about the group (Kind event).
type Message struct {
	ID          string       `json:"id"`
	At          time.Time    `json:"at"`
	Kind        string       `json:"kind"` // message | event
	FromMe      bool         `json:"from_me,omitempty"`
	Sender      *Person      `json:"sender,omitempty"` // who wrote it, when not the user
	Text        string       `json:"text,omitempty"`
	Service     string       `json:"service,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
	ReplyTo     string       `json:"reply_to,omitempty"` // the start of the message it answers
	Edited      bool         `json:"edited,omitempty"`
	Unsent      bool         `json:"unsent,omitempty"`
	Unread      bool         `json:"unread,omitempty"`
	// Undecoded: the text is archived in a form this reader does not understand; the app says so.
	Undecoded bool `json:"undecoded,omitempty"`
}

// Attachment is described, not copied: its file stays on the Mac.
type Attachment struct {
	Kind string `json:"kind"` // image | video | audio | file
	Name string `json:"name,omitempty"`
	Mime string `json:"mime,omitempty"`
	Size int64  `json:"size,omitempty"`
}

// ChatsPage is a page of conversations, newest first; Before asks for the next one.
type ChatsPage struct {
	Chats  []Chat `json:"chats"`
	More   bool   `json:"more,omitempty"`
	Before string `json:"before,omitempty"`
}

// MessagesPage is a page of one conversation's messages, oldest first; Before asks for the older ones.
type MessagesPage struct {
	Chat     Chat      `json:"chat"`
	Messages []Message `json:"messages"`
	More     bool      `json:"more,omitempty"`
	Before   string    `json:"before,omitempty"`
}

var (
	// ErrNoPermission: macOS does not let this process read the Messages data (Full Disk Access).
	ErrNoPermission = errors.New("no permission to read the Messages data")
	// ErrNoChat: no conversation has that id.
	ErrNoChat = errors.New("no such conversation")
)

func (r *Reader) chatDB() string { return filepath.Join(r.Home, "Library", "Messages", "chat.db") }

func (r *Reader) sqlite() string {
	if r.SQLite != "" {
		return r.SQLite
	}
	return "/usr/bin/sqlite3"
}

// Status checks that chat.db is there and readable.
func (r *Reader) Status(ctx context.Context) Status {
	st := Status{State: connector.StateOK, Via: r.Via}
	if _, err := os.Stat(r.chatDB()); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return r.denied(st)
		}
		st.State, st.Error = connector.StateNotConfigured, "这台电脑上没有「信息」的数据"
		return st
	}
	var rows []struct{ N int }
	if err := r.query(ctx, r.chatDB(), "SELECT count(*) AS n FROM chat", &rows); err != nil {
		if errors.Is(err, ErrNoPermission) {
			return r.denied(st)
		}
		st.State, st.Error = connector.StateError, "读不了「信息」的数据："+err.Error()
	}
	return st
}

func (r *Reader) denied(st Status) Status {
	st.State = connector.StateNoPermission
	st.Error = "macOS 不让 Jarvis 读这台 Mac 上的「信息」：要给它「完全磁盘访问权限」"
	if r.Via == "launchd" {
		exe := r.Executable
		if exe == "" {
			exe = "jarvisd"
		}
		st.Guide = []string{
			"在这台 Mac 上打开「系统设置 › 隐私与安全性 › 完全磁盘访问权限」",
			"点「+」，按 ⇧⌘G 输入 " + exe + "，选中它，打开它的开关",
			"回到 Jarvis 刷新。jarvisd 更新后如果又读不了，把它的开关关了再打开",
		}
	} else {
		st.Guide = []string{
			"在这台 Mac 上打开「系统设置 › 通用 › 共享」",
			"点「远程登录」右边的 ⓘ，打开「允许远程用户完全访问磁盘」",
			"回到 Jarvis 刷新。注意：这样经 ssh 登录这台 Mac 的任何会话都能读整个磁盘",
		}
	}
	return st
}

// query runs one SELECT read-only and decodes its rows (sqlite3's -json) into out, a pointer to a slice of structs.
// Every value spliced into SQL is an integer or a checked identifier: nothing from a request reaches it as text.
func (r *Reader) query(ctx context.Context, db, sql string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.sqlite(), "-readonly", "-bail", "-json", "-cmd", ".timeout 3000", db)
	cmd.Stdin = strings.NewReader(sql)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		msg := strings.ToLower(stderr.String())
		switch {
		case strings.Contains(msg, "authorization denied"), strings.Contains(msg, "not permitted"),
			strings.Contains(msg, "unable to open database"):
			return ErrNoPermission
		case ctx.Err() != nil:
			return fmt.Errorf("sqlite3 took too long")
		}
		// The error names the database's path at most, never a row.
		return fmt.Errorf("sqlite3: %s", firstLine(stderr.String(), err))
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil // no rows: sqlite3 prints nothing
	}
	return json.Unmarshal(raw, out)
}

func firstLine(s string, err error) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	if s == "" {
		s = err.Error()
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// appleEpoch is 2001-01-01 UTC, where Messages counts its dates from: nanoseconds since macOS 10.13, seconds before.
const appleEpoch = 978307200

func appleTime(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	if v > 1e11 || v < -1e11 {
		return time.Unix(appleEpoch+v/1e9, v%1e9).UTC()
	}
	return time.Unix(appleEpoch+v, 0).UTC()
}

// chatRow is one row of Messages' chat table with its newest message's date.
type chatRow struct {
	RowID   int64   `json:"rowid"`
	GUID    string  `json:"guid"`
	Style   int     `json:"style"`
	Ident   *string `json:"ident"`
	Service *string `json:"service"`
	Name    *string `json:"name"`
	Last    int64   `json:"last"`
}

// conversation is the chats of one conversation (a person's chats over each service, or one group).
type conversation struct {
	id      string
	group   bool
	name    string
	ident   string // a one-to-one chat's number or address
	chats   []int64
	service []string
	last    int64
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// convID hashes what identifies a conversation, so the id says nothing about who is in it.
func convID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return ID + "-" + hex.EncodeToString(sum[:6])
}

// conversations groups every chat with a message into conversations, newest first.
func (r *Reader) conversations(ctx context.Context) ([]*conversation, error) {
	var rows []chatRow
	err := r.query(ctx, r.chatDB(), `SELECT c.ROWID AS rowid, c.guid AS guid, c.style AS style, c.chat_identifier AS ident,
  c.service_name AS service, c.display_name AS name, j.last AS last
FROM chat c JOIN (SELECT chat_id, max(message_date) AS last FROM chat_message_join GROUP BY chat_id) j ON j.chat_id = c.ROWID`, &rows)
	if err != nil {
		return nil, err
	}
	byKey := map[string]*conversation{}
	var out []*conversation
	for _, c := range rows {
		group := c.Style == 43
		key := "group:" + c.GUID
		if !group {
			key = "person:" + strings.ToLower(str(c.Ident))
		}
		cv := byKey[key]
		if cv == nil {
			cv = &conversation{id: convID(key), group: group, ident: str(c.Ident)}
			byKey[key] = cv
			out = append(out, cv)
		}
		cv.chats = append(cv.chats, c.RowID)
		if s := str(c.Service); s != "" && !contains(cv.service, s) {
			cv.service = append(cv.service, s)
		}
		if c.Last > cv.last {
			cv.last = c.Last
		}
		if n := strings.TrimSpace(str(c.Name)); n != "" && cv.name == "" {
			cv.name = n
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].last != out[j].last {
			return out[i].last > out[j].last
		}
		return out[i].id < out[j].id
	})
	for _, cv := range out {
		sort.Strings(cv.service)
	}
	return out, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func ints(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ",")
}

// msgRow is one message as the queries select it.
type msgRow struct {
	Conv        int     `json:"conv"`
	ChatRow     int64   `json:"chat"` // the chat's ROWID (Changes, Search)
	RowID       int64   `json:"rowid"`
	GUID        string  `json:"guid"`
	Date        int64   `json:"date"`
	Text        *string `json:"text"`
	Body        *string `json:"body"` // hex(attributedBody)
	FromMe      int     `json:"me"`
	Handle      int64   `json:"handle"`
	Service     *string `json:"service"`
	Read        int     `json:"read"`
	Item        int     `json:"item"`
	Action      int     `json:"action"`
	Other       int64   `json:"other"`
	Title       *string `json:"title"`
	Attachments int     `json:"att"`
	Audio       int     `json:"audio"`
	Thread      *string `json:"thread"`
	Edited      int64   `json:"edited"`
	Retracted   int64   `json:"retracted"`
	Balloon     *string `json:"balloon"`
}

const msgColumns = `m.ROWID AS rowid, m.guid AS guid, m.date AS date, m.text AS text, hex(m.attributedBody) AS body,
  m.is_from_me AS me, m.handle_id AS handle, m.service AS service, m.is_read AS read, m.item_type AS item,
  m.group_action_type AS action, m.other_handle AS other, m.group_title AS title, m.cache_has_attachments AS att,
  m.is_audio_message AS audio, m.thread_originator_guid AS thread, m.date_edited AS edited,
  m.date_retracted AS retracted, m.balloon_bundle_id AS balloon`

// text is a message's words: its text column, or what its attributedBody holds. ok is false when neither can be
// read (the app shows that it could not).
func (m msgRow) text() (string, bool) {
	t := str(m.Text)
	if strings.TrimSpace(strings.ReplaceAll(t, "￼", "")) == "" && m.Body != nil && *m.Body != "" {
		blob, err := hex.DecodeString(*m.Body)
		if err != nil {
			return "", false
		}
		if t, err = bodyText(blob); err != nil {
			return "", false
		}
	}
	return strings.TrimSpace(strings.ReplaceAll(t, "￼", "")), true
}

// Chats is a page of conversations, newest first, before the cursor a previous page returned. keep KeepContacts
// leaves out what does not go to Jarvis (Changes); anything else lists them all (the app's own view).
func (r *Reader) Chats(ctx context.Context, limit int, before, keep string) (ChatsPage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	convs, err := r.conversations(ctx)
	if err != nil {
		return ChatsPage{}, err
	}
	start := 0
	if before != "" {
		at, id, ok := parseChatCursor(before)
		if !ok {
			return ChatsPage{}, fmt.Errorf("bad cursor")
		}
		start = sort.Search(len(convs), func(i int) bool {
			return convs[i].last < at || convs[i].last == at && convs[i].id > id
		})
	}
	page := convs[start:min(len(convs), start+limit)]
	out := ChatsPage{Chats: []Chat{}, More: start+limit < len(convs)}
	if len(page) == 0 {
		return out, nil
	}
	if out.More {
		last := page[len(page)-1]
		out.Before = fmt.Sprintf("%d.%s", last.last, last.id)
	}
	people, err := r.members(ctx, page)
	if err != nil {
		return ChatsPage{}, err
	}
	previews, err := r.previews(ctx, page)
	if err != nil {
		return ChatsPage{}, err
	}
	unread, err := r.unread(ctx, page)
	if err != nil {
		return ChatsPage{}, err
	}
	names := r.names(ctx)
	handles, err := r.handles(ctx, previewHandles(previews))
	if err != nil {
		return ChatsPage{}, err
	}
	var withFiles []int64
	for _, p := range previews {
		if p.Attachments != 0 {
			withFiles = append(withFiles, p.RowID)
		}
	}
	files, err := r.attachments(ctx, withFiles)
	if err != nil {
		return ChatsPage{}, err
	}
	wrote := map[string]bool{}
	if keep == KeepContacts {
		if wrote, err = r.wrote(ctx, page); err != nil {
			return ChatsPage{}, err
		}
	}
	for i, cv := range page {
		c := r.chat(cv, people[cv.id], names)
		if keep == KeepContacts && !wrote[cv.id] && !anyNamed(c.Members) {
			continue
		}
		if p, ok := previews[i]; ok {
			c.FromMe = p.FromMe == 1
			c.Preview = preview(p, names, handles, files[p.RowID], c.Group)
		}
		c.Unread = unread[i]
		out.Chats = append(out.Chats, c)
	}
	return out, nil
}

func parseChatCursor(s string) (int64, string, bool) {
	at, id, ok := strings.Cut(s, ".")
	n, err := strconv.ParseInt(at, 10, 64)
	return n, id, ok && err == nil
}

// chat is a conversation as the app shows it: a group's name, or else its people's names.
func (r *Reader) chat(cv *conversation, members []Person, names *contacts) Chat {
	c := Chat{ID: cv.id, Group: cv.group, Members: members, Services: cv.service, Last: appleTime(cv.last), Name: cv.name}
	if c.Members == nil {
		c.Members = []Person{}
	}
	if c.Services == nil {
		c.Services = []string{}
	}
	if c.Name == "" {
		var shown []string
		for _, p := range members {
			shown = append(shown, p.shown())
		}
		switch {
		case len(shown) == 0 && !cv.group && cv.ident != "":
			c.Name = names.name(cv.ident)
			if c.Name == "" {
				c.Name = cv.ident
			}
		case len(shown) == 0:
			c.Name = "（没有成员）"
		case !cv.group || len(shown) <= 3:
			c.Name = strings.Join(shown, "、")
		default:
			c.Name = fmt.Sprintf("%s等 %d 人", strings.Join(shown[:3], "、"), len(shown))
		}
	}
	return c
}

func (p Person) shown() string {
	if p.Name != "" {
		return p.Name
	}
	return p.Handle
}

// members are each conversation's people (the user is never one of them), by conversation id.
func (r *Reader) members(ctx context.Context, convs []*conversation) (map[string][]Person, error) {
	var chats []int64
	owner := map[int64]string{}
	for _, cv := range convs {
		for _, id := range cv.chats {
			chats = append(chats, id)
			owner[id] = cv.id
		}
	}
	var rows []struct {
		Chat   int64 `json:"chat"`
		Handle int64 `json:"handle"`
	}
	if err := r.query(ctx, r.chatDB(), "SELECT chat_id AS chat, handle_id AS handle FROM chat_handle_join WHERE chat_id IN ("+
		ints(chats)+") ORDER BY chat_id, ROWID", &rows); err != nil {
		return nil, err
	}
	var ids []int64
	for _, row := range rows {
		ids = append(ids, row.Handle)
	}
	handles, err := r.handles(ctx, ids)
	if err != nil {
		return nil, err
	}
	names := r.names(ctx)
	out := map[string][]Person{}
	seen := map[string]bool{}
	for _, row := range rows {
		h, ok := handles[row.Handle]
		if !ok {
			continue
		}
		conv := owner[row.Chat]
		// The same person over iMessage and SMS is two handles with one address.
		if key := conv + "\x00" + strings.ToLower(h); !seen[key] {
			seen[key] = true
			out[conv] = append(out[conv], Person{Handle: h, Name: names.name(h)})
		}
	}
	return out, nil
}

// handles are the numbers and addresses of handle rows.
func (r *Reader) handles(ctx context.Context, ids []int64) (map[int64]string, error) {
	out := map[int64]string{}
	var want []int64
	for _, id := range ids {
		if id > 0 {
			want = append(want, id)
		}
	}
	if len(want) == 0 {
		return out, nil
	}
	var rows []struct {
		RowID int64  `json:"rowid"`
		ID    string `json:"id"`
	}
	if err := r.query(ctx, r.chatDB(), "SELECT ROWID AS rowid, id FROM handle WHERE ROWID IN ("+ints(want)+")", &rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.RowID] = row.ID
	}
	return out, nil
}

// previews are each conversation's newest message that is not a tapback, by index in convs.
func (r *Reader) previews(ctx context.Context, convs []*conversation) (map[int]msgRow, error) {
	parts := make([]string, len(convs))
	for i, cv := range convs {
		parts[i] = fmt.Sprintf(`SELECT * FROM (SELECT %d AS conv, %s FROM chat_message_join j JOIN message m ON m.ROWID = j.message_id
  WHERE j.chat_id IN (%s) AND m.associated_message_type = 0 ORDER BY j.message_date DESC, j.message_id DESC LIMIT 1)`, i, msgColumns, ints(cv.chats))
	}
	var rows []msgRow
	if err := r.query(ctx, r.chatDB(), strings.Join(parts, "\nUNION ALL\n"), &rows); err != nil {
		return nil, err
	}
	out := map[int]msgRow{}
	for _, row := range rows {
		out[row.Conv] = row
	}
	return out, nil
}

func previewHandles(rows map[int]msgRow) []int64 {
	var out []int64
	for _, row := range rows {
		out = append(out, row.Handle, row.Other)
	}
	return out
}

// preview is one line for the list: the text, or what the message is.
func preview(m msgRow, names *contacts, handles map[int64]string, files []Attachment, group bool) string {
	msg := message(m, names, handles, files)
	text := msg.Text
	switch {
	case msg.Unsent:
		text = "撤回了一条消息"
	case msg.Undecoded:
		text = "[无法显示的消息]"
	case text == "" && len(msg.Attachments) > 0:
		text = msg.Attachments[0].placeholder()
	}
	if group && msg.Kind == "message" && !msg.FromMe && msg.Sender != nil {
		text = msg.Sender.shown() + "：" + text
	}
	if r := []rune(text); len(r) > 80 {
		text = string(r[:80]) + "…"
	}
	return text
}

func (a Attachment) placeholder() string {
	switch a.Kind {
	case "image":
		return "[图片]"
	case "video":
		return "[视频]"
	case "audio":
		return "[语音]"
	}
	return "[文件]"
}

// unread counts each conversation's messages from others not read yet, by index in convs.
func (r *Reader) unread(ctx context.Context, convs []*conversation) (map[int]int, error) {
	var chats []int64
	index := map[int64]int{}
	for i, cv := range convs {
		for _, id := range cv.chats {
			chats = append(chats, id)
			index[id] = i
		}
	}
	var rows []struct {
		Chat int64 `json:"chat"`
		N    int   `json:"n"`
	}
	if err := r.query(ctx, r.chatDB(), `SELECT j.chat_id AS chat, count(*) AS n FROM chat_message_join j JOIN message m ON m.ROWID = j.message_id
WHERE j.chat_id IN (`+ints(chats)+`) AND m.is_read = 0 AND m.is_from_me = 0 AND m.item_type = 0 AND m.associated_message_type = 0
GROUP BY j.chat_id`, &rows); err != nil {
		return nil, err
	}
	out := map[int]int{}
	for _, row := range rows {
		out[index[row.Chat]] += row.N
	}
	return out, nil
}

// Messages is a page of a conversation's messages, the newest ones or those before the cursor a previous page
// returned, oldest first. Tapbacks are left out. keep KeepContacts refuses a conversation that does not go to Jarvis.
func (r *Reader) Messages(ctx context.Context, id string, limit int, before, keep string) (MessagesPage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	convs, err := r.conversations(ctx)
	if err != nil {
		return MessagesPage{}, err
	}
	var cv *conversation
	for _, c := range convs {
		if c.id == id {
			cv = c
			break
		}
	}
	if cv == nil {
		return MessagesPage{}, ErrNoChat
	}
	if keep == KeepContacts {
		people, err := r.members(ctx, []*conversation{cv})
		if err != nil {
			return MessagesPage{}, err
		}
		wrote, err := r.wrote(ctx, []*conversation{cv})
		if err != nil {
			return MessagesPage{}, err
		}
		if !wrote[cv.id] && !anyNamed(people[cv.id]) {
			return MessagesPage{}, ErrNoChat
		}
	}
	where := ""
	if before != "" {
		at, row, ok := strings.Cut(before, ".")
		d, err1 := strconv.ParseInt(at, 10, 64)
		n, err2 := strconv.ParseInt(row, 10, 64)
		if !ok || err1 != nil || err2 != nil {
			return MessagesPage{}, fmt.Errorf("bad cursor")
		}
		where = fmt.Sprintf(" AND (j.message_date < %d OR j.message_date = %d AND j.message_id < %d)", d, d, n)
	}
	var rows []msgRow
	if err := r.query(ctx, r.chatDB(), fmt.Sprintf(`SELECT 0 AS conv, %s FROM chat_message_join j JOIN message m ON m.ROWID = j.message_id
WHERE j.chat_id IN (%s) AND m.associated_message_type = 0%s ORDER BY j.message_date DESC, j.message_id DESC LIMIT %d`,
		msgColumns, ints(cv.chats), where, limit+1), &rows); err != nil {
		return MessagesPage{}, err
	}
	page := MessagesPage{Messages: []Message{}}
	if len(rows) > limit {
		rows, page.More = rows[:limit], true
	}
	if page.More {
		oldest := rows[len(rows)-1]
		page.Before = fmt.Sprintf("%d.%d", oldest.Date, oldest.RowID)
	}
	var ids []int64
	var withFiles []int64
	var threads []string
	for _, m := range rows {
		ids = append(ids, m.Handle, m.Other)
		if m.Attachments != 0 {
			withFiles = append(withFiles, m.RowID)
		}
		if t := str(m.Thread); t != "" {
			threads = append(threads, t)
		}
	}
	handles, err := r.handles(ctx, ids)
	if err != nil {
		return MessagesPage{}, err
	}
	files, err := r.attachments(ctx, withFiles)
	if err != nil {
		return MessagesPage{}, err
	}
	replied, err := r.replied(ctx, threads)
	if err != nil {
		return MessagesPage{}, err
	}
	names := r.names(ctx)
	people, err := r.members(ctx, []*conversation{cv})
	if err != nil {
		return MessagesPage{}, err
	}
	page.Chat = r.chat(cv, people[cv.id], names)
	for i := len(rows) - 1; i >= 0; i-- {
		m := message(rows[i], names, handles, files[rows[i].RowID])
		if t := str(rows[i].Thread); t != "" {
			m.ReplyTo = replied[t]
		}
		page.Messages = append(page.Messages, m)
	}
	return page, nil
}

// message turns a row into what the app shows.
func message(m msgRow, names *contacts, handles map[int64]string, files []Attachment) Message {
	out := Message{ID: strconv.FormatInt(m.RowID, 10), At: appleTime(m.Date), Kind: "message", FromMe: m.FromMe == 1,
		Service: str(m.Service), Edited: m.Edited != 0, Unsent: m.Retracted != 0, Unread: m.FromMe == 0 && m.Read == 0,
		Attachments: files}
	if !out.FromMe {
		if h, ok := handles[m.Handle]; ok {
			out.Sender = &Person{Handle: h, Name: names.name(h)}
		}
	}
	who := "你"
	if out.Sender != nil {
		who = out.Sender.shown()
	} else if !out.FromMe {
		who = "有人"
	}
	switch m.Item {
	case 0:
	case 1:
		out.Kind = "event"
		other := "有人"
		if h, ok := handles[m.Other]; ok {
			other = Person{Handle: h, Name: names.name(h)}.shown()
		}
		if m.Action == 1 {
			out.Text = who + "把" + other + "移出了群聊"
		} else {
			out.Text = who + "把" + other + "加进了群聊"
		}
		return out
	case 2:
		out.Kind = "event"
		if t := strings.TrimSpace(str(m.Title)); t != "" {
			out.Text = who + "把群名改成「" + t + "」"
		} else {
			out.Text = who + "去掉了群名"
		}
		return out
	case 3:
		out.Kind = "event"
		switch m.Action {
		case 0:
			out.Text = who + "离开了群聊"
		case 1:
			out.Text = who + "换了群头像"
		case 2:
			out.Text = who + "去掉了群头像"
		default:
			out.Text = "群聊有变动"
		}
		return out
	default: // FaceTime, location sharing and the like
		out.Kind = "event"
		out.Text = "[系统消息]"
		return out
	}
	if out.Unsent {
		return out
	}
	text, ok := m.text()
	out.Text = text
	out.Undecoded = !ok
	if m.Audio != 0 {
		for i := range out.Attachments {
			out.Attachments[i].Kind = "audio"
		}
	}
	if out.Text == "" && len(out.Attachments) == 0 && !out.Undecoded && str(m.Balloon) != "" {
		out.Text = "[来自 App 的消息]"
	}
	return out
}

// attachments describe the files of messages, by message ROWID.
func (r *Reader) attachments(ctx context.Context, messages []int64) (map[int64][]Attachment, error) {
	out := map[int64][]Attachment{}
	if len(messages) == 0 {
		return out, nil
	}
	var rows []struct {
		Message int64   `json:"message"`
		Name    *string `json:"name"`
		Mime    *string `json:"mime"`
		Size    int64   `json:"size"`
	}
	if err := r.query(ctx, r.chatDB(), `SELECT j.message_id AS message, a.transfer_name AS name, a.mime_type AS mime, a.total_bytes AS size
FROM message_attachment_join j JOIN attachment a ON a.ROWID = j.attachment_id
WHERE j.message_id IN (`+ints(messages)+`) AND a.hide_attachment = 0 ORDER BY j.message_id, a.ROWID`, &rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		mime := str(row.Mime)
		kind := "file"
		switch {
		case strings.HasPrefix(mime, "image/"):
			kind = "image"
		case strings.HasPrefix(mime, "video/"):
			kind = "video"
		case strings.HasPrefix(mime, "audio/"):
			kind = "audio"
		}
		out[row.Message] = append(out[row.Message], Attachment{Kind: kind, Name: str(row.Name), Mime: mime, Size: row.Size})
	}
	return out, nil
}

// guidLike is what a message guid looks like; anything else is not spliced into SQL.
func guidLike(s string) bool {
	if s == "" || len(s) > 80 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-' || c == ':' || c == '/' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}

// replied are the starts of the messages that others answer in a thread, by guid.
func (r *Reader) replied(ctx context.Context, guids []string) (map[string]string, error) {
	out := map[string]string{}
	var quoted []string
	for _, g := range guids {
		if guidLike(g) {
			quoted = append(quoted, "'"+g+"'")
		}
	}
	if len(quoted) == 0 {
		return out, nil
	}
	var rows []msgRow
	if err := r.query(ctx, r.chatDB(), "SELECT 0 AS conv, "+msgColumns+" FROM message m WHERE m.guid IN ("+strings.Join(quoted, ",")+")", &rows); err != nil {
		return nil, err
	}
	for _, m := range rows {
		text, ok := m.text()
		if !ok || text == "" {
			continue
		}
		if r := []rune(text); len(r) > 40 {
			text = string(r[:40]) + "…"
		}
		out[m.GUID] = text
	}
	return out, nil
}

// names are this Mac's contacts, read again every few minutes.
func (r *Reader) names(ctx context.Context) *contacts {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.contacts != nil && time.Since(r.loaded) < 5*time.Minute {
		return r.contacts
	}
	c, err := r.loadContacts(ctx)
	if err != nil {
		c = &contacts{} // no names: numbers are shown instead
	}
	r.contacts, r.loaded = c, time.Now()
	return c
}
