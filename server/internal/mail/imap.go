package mail

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	gomessage "github.com/emersion/go-message"
	gomail "github.com/emersion/go-message/mail"
)

const (
	defaultListLimit = 50
	maxListLimit     = 200
	snippetRunes     = 160
	// snippetBytes is how much of the message body a list fetch asks for.
	// It is a BODY.PEEK partial section, so the rest never leaves the server.
	snippetBytes = 2048
)

// accountMutex returns the mutex serializing IMAP use of one account. IMAP
// connections are stateful (SELECT), so commands must not interleave.
func (s *Service) accountMutex(id string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.opMu == nil {
		s.opMu = make(map[string]*sync.Mutex)
	}
	m, ok := s.opMu[id]
	if !ok {
		m = &sync.Mutex{}
		s.opMu[id] = m
	}
	return m
}

// Mailboxes lists the mailboxes of an account with their role and counters.
func (s *Service) Mailboxes(ctx context.Context, id string) ([]Mailbox, error) {
	a, err := s.account(id)
	if err != nil {
		return nil, err
	}
	statusOpts := &imap.StatusOptions{NumMessages: true, NumUnseen: true}
	var out []Mailbox
	err = s.withConn(ctx, a, func(c *imapclient.Client) error {
		out = nil
		data, err := c.List("", "*", &imap.ListOptions{
			ReturnSpecialUse: true,
			ReturnStatus:     statusOpts,
		}).Collect()
		if err != nil {
			return err
		}
		needStatus := make([]*imap.ListData, 0, len(data))
		for _, ld := range data {
			if hasAttr(ld.Attrs, imap.MailboxAttrNoSelect) || hasAttr(ld.Attrs, imap.MailboxAttrNonExistent) {
				continue
			}
			mb := Mailbox{Name: ld.Mailbox, Role: roleOf(ld.Mailbox, ld.Attrs)}
			if ld.Status != nil {
				if ld.Status.NumMessages != nil {
					mb.Total = int(*ld.Status.NumMessages)
				}
				if ld.Status.NumUnseen != nil {
					mb.Unseen = int(*ld.Status.NumUnseen)
				}
				out = append(out, mb)
			} else {
				needStatus = append(needStatus, ld)
				out = append(out, mb)
			}
		}
		// Servers without LIST-STATUS: fall back to one STATUS per mailbox.
		for i, ld := range needStatus {
			st, err := c.Status(ld.Mailbox, statusOpts).Wait()
			if err != nil {
				continue
			}
			for j := range out {
				if out[j].Name == ld.Mailbox {
					if st.NumMessages != nil {
						out[j].Total = int(*st.NumMessages)
					}
					if st.NumUnseen != nil {
						out[j].Unseen = int(*st.NumUnseen)
					}
				}
			}
			_ = i
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// roleOf maps a mailbox name and its SPECIAL-USE attributes to a role.
func roleOf(name string, attrs []imap.MailboxAttr) string {
	if strings.EqualFold(name, "INBOX") {
		return RoleInbox
	}
	for _, attr := range attrs {
		switch attr {
		case imap.MailboxAttrSent:
			return RoleSent
		case imap.MailboxAttrDrafts:
			return RoleDrafts
		case imap.MailboxAttrTrash:
			return RoleTrash
		case imap.MailboxAttrArchive:
			return RoleArchive
		case imap.MailboxAttrJunk:
			return RoleJunk
		case imap.MailboxAttrAll:
			// Gmail's "All Mail". Not one of the documented roles, but useful
			// to the app, which can ignore unknown roles.
			return "all"
		}
	}
	// Providers that do not set SPECIAL-USE: guess from the name.
	switch strings.ToLower(name) {
	case "sent", "sent items", "sent messages", "sent mail":
		return RoleSent
	case "drafts", "draft":
		return RoleDrafts
	case "trash", "deleted items", "deleted messages", "bin":
		return RoleTrash
	case "archive", "archives":
		return RoleArchive
	case "junk", "spam", "junk e-mail", "bulk mail":
		return RoleJunk
	}
	return ""
}

func hasAttr(attrs []imap.MailboxAttr, want imap.MailboxAttr) bool {
	for _, a := range attrs {
		if a == want {
			return true
		}
	}
	return false
}

// ListMessages lists messages of one mailbox, or of every account's inbox when
// opts.AccountID is empty.
func (s *Service) ListMessages(ctx context.Context, opts ListOptions) (*ListResult, error) {
	limit := opts.Limit
	switch {
	case limit <= 0:
		limit = defaultListLimit
	case limit > maxListLimit:
		limit = maxListLimit
	}
	mailbox := strings.TrimSpace(opts.Mailbox)
	if mailbox == "" {
		mailbox = "INBOX"
	}

	if opts.AccountID != "" {
		a, err := s.account(opts.AccountID)
		if err != nil {
			return nil, err
		}
		return s.listOne(ctx, a, mailbox, limit, opts.BeforeUID, opts.Query)
	}

	s.mu.Lock()
	accounts := s.snapshotLocked()
	s.mu.Unlock()

	type result struct {
		res *ListResult
		err error
	}
	results := make([]result, len(accounts))
	var wg sync.WaitGroup
	for i, a := range accounts {
		wg.Add(1)
		go func(i int, a *MailAccount) {
			defer wg.Done()
			res, err := s.listOne(ctx, a, "INBOX", limit, opts.BeforeUID, opts.Query)
			results[i] = result{res, err}
		}(i, a)
	}
	wg.Wait()

	var all []MailSummary
	hasMore := false
	var firstErr error
	for _, r := range results {
		if r.err != nil {
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		all = append(all, r.res.Messages...)
		hasMore = hasMore || r.res.HasMore
	}
	if len(all) == 0 && firstErr != nil {
		return nil, firstErr
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Date != all[j].Date {
			return all[i].Date > all[j].Date // RFC3339 sorts lexicographically
		}
		return all[i].UID > all[j].UID
	})
	if len(all) > limit {
		all = all[:limit]
		hasMore = true
	}
	return &ListResult{Messages: all, HasMore: hasMore}, nil
}

// searchUIDs returns matching UIDs sorted ascending.
func (s *Service) searchUIDs(ctx context.Context, c *imapclient.Client, a *MailAccount, mailbox string, beforeUID uint32, query string) ([]imap.UID, error) {
	if query != "" && a.Provider == ProviderGmail {
		// Gmail query syntax needs X-GM-RAW, which the IMAP client library
		// cannot express; use a short-lived raw connection.
		if uids, err := s.gmailSearch(ctx, a, mailbox, query, beforeUID); err == nil {
			return uids, nil
		} else {
			logf("mail: X-GM-RAW search failed, falling back to TEXT search: %v", err)
		}
	}
	criteria := &imap.SearchCriteria{}
	if beforeUID > 1 {
		var set imap.UIDSet
		set.AddRange(1, imap.UID(beforeUID-1))
		criteria.UID = append(criteria.UID, set)
	}
	if query != "" {
		criteria.Text = append(criteria.Text, query)
	}
	data, err := c.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return nil, err
	}
	uids := data.AllUIDs()
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	return uids, nil
}

// listOne pages one mailbox of one account.
func (s *Service) listOne(ctx context.Context, a *MailAccount, mailbox string, limit int, beforeUID uint32, query string) (*ListResult, error) {
	var out *ListResult
	err := s.withConn(ctx, a, func(c *imapclient.Client) error {
		out = nil
		if _, err := c.Select(mailbox, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
			return fmt.Errorf("select %s: %w", mailbox, err)
		}
		uids, err := s.searchUIDs(ctx, c, a, mailbox, beforeUID, query)
		if err != nil {
			return fmt.Errorf("search: %w", err)
		}
		hasMore := len(uids) > limit
		if hasMore {
			uids = uids[len(uids)-limit:]
		}
		res := &ListResult{Messages: []MailSummary{}, HasMore: hasMore}
		if len(uids) == 0 {
			out = res
			return nil
		}

		var set imap.UIDSet
		set.AddNum(uids...)
		msgs, err := c.Fetch(set, &imap.FetchOptions{
			UID:           true,
			Envelope:      true,
			Flags:         true,
			InternalDate:  true,
			RFC822Size:    true,
			BodyStructure: &imap.FetchItemBodyStructure{Extended: true},
			BodySection: []*imap.FetchItemBodySection{
				{Specifier: imap.PartSpecifierHeader, HeaderFields: []string{"Content-Type", "Content-Transfer-Encoding"}, Peek: true},
				{Specifier: imap.PartSpecifierText, Partial: &imap.SectionPartial{Offset: 0, Size: snippetBytes}, Peek: true},
			},
		}).Collect()
		if err != nil {
			return fmt.Errorf("fetch: %w", err)
		}
		for _, m := range msgs {
			res.Messages = append(res.Messages, s.summaryFrom(a, mailbox, m))
		}
		sort.SliceStable(res.Messages, func(i, j int) bool {
			return res.Messages[i].UID > res.Messages[j].UID
		})
		out = res
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// summaryFrom converts one fetched message into a summary row.
func (s *Service) summaryFrom(a *MailAccount, mailbox string, m *imapclient.FetchMessageBuffer) MailSummary {
	sum := MailSummary{
		AccountID: a.ID,
		Mailbox:   mailbox,
		UID:       uint32(m.UID),
		Seen:      hasFlag(m.Flags, imap.FlagSeen),
		Flagged:   hasFlag(m.Flags, imap.FlagFlagged),
	}
	if m.Envelope != nil {
		sum.Subject = m.Envelope.Subject
		sum.MessageID = m.Envelope.MessageID
		sum.From = firstAddress(m.Envelope.From)
		for _, addr := range m.Envelope.To {
			if e := addr.Addr(); e != "" {
				sum.To = append(sum.To, Address{Name: addr.Name, Email: e})
			}
		}
		date := m.Envelope.Date
		if date.IsZero() {
			date = m.InternalDate
		}
		if !date.IsZero() {
			sum.Date = date.UTC().Format(time.RFC3339)
		}
	} else if !m.InternalDate.IsZero() {
		sum.Date = m.InternalDate.UTC().Format(time.RFC3339)
	}
	sum.HasAttach = bodyStructureHasAttachments(m.BodyStructure)
	sum.Snippet = snippetOf(m.BodySection)
	return sum
}

func firstAddress(addrs []imap.Address) Address {
	for _, addr := range addrs {
		if e := addr.Addr(); e != "" {
			return Address{Name: addr.Name, Email: e}
		}
	}
	return Address{}
}

func hasFlag(flags []imap.Flag, want imap.Flag) bool {
	for _, f := range flags {
		if f == want {
			return true
		}
	}
	return false
}

// bodyStructureHasAttachments reports whether any part is a non-inline
// attachment.
func bodyStructureHasAttachments(bs imap.BodyStructure) bool {
	if bs == nil {
		return false
	}
	found := false
	bs.Walk(func(path []int, part imap.BodyStructure) bool {
		if found {
			return false
		}
		single, ok := part.(*imap.BodyStructureSinglePart)
		if !ok {
			return true
		}
		if disp := single.Disposition(); disp != nil {
			if strings.EqualFold(disp.Value, "attachment") {
				found = true
				return false
			}
			if strings.EqualFold(disp.Value, "inline") {
				return true
			}
		}
		if single.Filename() != "" && !strings.HasPrefix(single.MediaType(), "text/") {
			found = true
			return false
		}
		return true
	})
	return found
}

// snippetOf turns the fetched header + body sections into a short preview.
func snippetOf(sections []imapclient.FetchBodySectionBuffer) string {
	var header, body []byte
	for _, sec := range sections {
		if sec.Section == nil {
			continue
		}
		switch sec.Section.Specifier {
		case imap.PartSpecifierHeader:
			header = sec.Bytes
		case imap.PartSpecifierText:
			body = sec.Bytes
		}
	}
	if len(body) == 0 {
		return ""
	}
	raw := append(append([]byte{}, header...), body...)
	text := textFromRaw(raw)
	if text == "" {
		text = textFromRaw(body) // no usable header: try the body alone
	}
	text = strings.Join(strings.Fields(text), " ")
	return truncateRunes(text, snippetRunes)
}

// textFromRaw extracts readable text from a possibly truncated raw message.
func textFromRaw(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	// Tolerate truncation and unknown charsets: message.Read still returns a
	// usable entity for the leading bytes.
	e, _ := gomessage.New(gomessage.Header{}, bytes.NewReader(raw))
	if e != nil {
		if r, _ := gomessage.Read(bytes.NewReader(raw)); r != nil {
			e = r
		}
	}
	if mr := e.MultipartReader(); mr != nil {
		for {
			p, err := mr.NextPart()
			if err != nil || p == nil {
				break
			}
			mt, _, _ := p.Header.ContentType()
			if !strings.HasPrefix(mt, "text/") {
				continue
			}
			body := readAllTolerant(p.Body, 8<<10)
			return renderText(mt, body)
		}
		return ""
	}
	mt, _, _ := e.Header.ContentType()
	if !strings.HasPrefix(mt, "text/") {
		return ""
	}
	return renderText(mt, readAllTolerant(e.Body, 8<<10))
}

// readAllTolerant reads at most n bytes, ignoring decode errors at a truncated
// tail.
func readAllTolerant(r io.Reader, n int) string {
	if r == nil {
		return ""
	}
	buf := &bytes.Buffer{}
	_, _ = io.CopyN(buf, r, int64(n))
	return buf.String()
}

// renderText converts text/plain or text/html to preview text.
func renderText(mediaType string, s string) string {
	if strings.HasPrefix(mediaType, "text/html") {
		return stripHTML(s)
	}
	return s
}

// stripHTML removes tags and script/style content and unescapes entities.
func stripHTML(s string) string {
	var b strings.Builder
	lower := strings.ToLower(s)
	i := 0
	for i < len(s) {
		if s[i] != '<' {
			b.WriteByte(s[i])
			i++
			continue
		}
		end := strings.IndexByte(s[i:], '>')
		if end < 0 {
			break
		}
		tag := lower[i : i+end+1]
		i += end + 1
		if strings.HasPrefix(tag, "<script") || strings.HasPrefix(tag, "<style") {
			closing := "</script"
			if strings.HasPrefix(tag, "<style") {
				closing = "</style"
			}
			if j := strings.Index(lower[i:], closing); j >= 0 {
				i += j
			}
		} else if strings.HasPrefix(tag, "<br") || strings.HasPrefix(tag, "</p") || strings.HasPrefix(tag, "</div") {
			b.WriteByte(' ')
		}
	}
	return html.UnescapeString(b.String())
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	rs := []rune(s)
	return string(rs[:n])
}

// GetMessage fetches one full message. It never marks the message as seen.
func (s *Service) GetMessage(ctx context.Context, accountID string, uid uint32, mailbox string) (*MailMessage, error) {
	a, err := s.account(accountID)
	if err != nil {
		return nil, err
	}
	if mailbox == "" {
		mailbox = "INBOX"
	}
	var out *MailMessage
	err = s.withConn(ctx, a, func(c *imapclient.Client) error {
		out = nil
		if _, err := c.Select(mailbox, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
			return fmt.Errorf("select %s: %w", mailbox, err)
		}
		msgs, err := c.Fetch(imap.UIDSetNum(imap.UID(uid)), &imap.FetchOptions{
			UID:          true,
			Envelope:     true,
			Flags:        true,
			InternalDate: true,
			RFC822Size:   true,
			BodySection:  []*imap.FetchItemBodySection{{Peek: true}},
		}).Collect()
		if err != nil {
			return fmt.Errorf("fetch: %w", err)
		}
		if len(msgs) == 0 {
			return fmt.Errorf("message %d not found in %s", uid, mailbox)
		}
		out = messageFrom(a, mailbox, msgs[0])
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// messageFrom parses a fully fetched message.
func messageFrom(a *MailAccount, mailbox string, m *imapclient.FetchMessageBuffer) *MailMessage {
	sum := MailSummary{
		AccountID: a.ID,
		Mailbox:   mailbox,
		UID:       uint32(m.UID),
		Seen:      hasFlag(m.Flags, imap.FlagSeen),
		Flagged:   hasFlag(m.Flags, imap.FlagFlagged),
	}
	if m.Envelope != nil {
		sum.Subject = m.Envelope.Subject
		sum.MessageID = m.Envelope.MessageID
		sum.From = firstAddress(m.Envelope.From)
		for _, addr := range m.Envelope.To {
			if e := addr.Addr(); e != "" {
				sum.To = append(sum.To, Address{Name: addr.Name, Email: e})
			}
		}
		if !m.Envelope.Date.IsZero() {
			sum.Date = m.Envelope.Date.UTC().Format(time.RFC3339)
		}
	}
	if sum.Date == "" && !m.InternalDate.IsZero() {
		sum.Date = m.InternalDate.UTC().Format(time.RFC3339)
	}
	msg := &MailMessage{MailSummary: sum, Attachments: []Attachment{}}

	var raw []byte
	for _, sec := range m.BodySection {
		if sec.Section != nil && len(sec.Section.Part) == 0 && sec.Section.Specifier == imap.PartSpecifierNone {
			raw = sec.Bytes
		}
	}
	if raw == nil && len(m.BodySection) > 0 {
		raw = m.BodySection[0].Bytes
	}
	parseMessage(raw, msg)
	sum.HasAttach = len(msg.Attachments) > 0
	msg.MailSummary = sum
	msg.Snippet = truncateRunes(strings.Join(strings.Fields(msg.Text), " "), snippetRunes)
	if msg.Snippet == "" {
		msg.Snippet = truncateRunes(strings.Join(strings.Fields(stripHTML(msg.HTML)), " "), snippetRunes)
	}
	return msg
}

// parseMessage fills text, html, attachments and threading headers.
func parseMessage(raw []byte, msg *MailMessage) {
	if len(raw) == 0 {
		return
	}
	// Headers first, so threading info survives a body parse failure.
	if h, err := readMailHeader(raw); err == nil {
		if ids, err := h.MsgIDList("In-Reply-To"); err == nil && len(ids) > 0 {
			msg.InReplyTo = ids[len(ids)-1]
		}
		if ids, err := h.MsgIDList("References"); err == nil {
			msg.References = ids
		}
		if len(msg.To) == 0 {
			if addrs, err := h.AddressList("To"); err == nil {
				for _, a := range addrs {
					msg.To = append(msg.To, Address{Name: a.Name, Email: a.Address})
				}
			}
		}
		if addrs, err := h.AddressList("Cc"); err == nil {
			for _, a := range addrs {
				msg.Cc = append(msg.Cc, Address{Name: a.Name, Email: a.Address})
			}
		}
	}

	r, err := gomail.CreateReader(bytes.NewReader(raw))
	if r == nil {
		_ = err
		return
	}
	defer r.Close()
	var htmlBody string
	for {
		part, err := r.NextPart()
		if err != nil {
			break
		}
		switch h := part.Header.(type) {
		case *gomail.InlineHeader:
			ct, _, _ := h.ContentType()
			body := readAllTolerant(part.Body, 8<<20)
			switch {
			case strings.HasPrefix(ct, "text/plain") && msg.Text == "":
				msg.Text = body
			case strings.HasPrefix(ct, "text/html") && htmlBody == "":
				htmlBody = body
			}
		case *gomail.AttachmentHeader:
			name, _ := h.Filename()
			ct, _, _ := h.ContentType()
			msg.Attachments = append(msg.Attachments, Attachment{
				Filename: name,
				Mime:     ct,
				Size:     partSize(h.Header.Get("Content-Transfer-Encoding"), part.Body),
			})
		}
	}
	if htmlBody != "" {
		msg.HTML = htmlBody
	}
	if msg.Text == "" && msg.HTML != "" {
		msg.Text = stripHTML(msg.HTML)
	}
	if msg.Attachments == nil {
		msg.Attachments = []Attachment{}
	}
}

// readMailHeader parses just the header block of a raw message.
func readMailHeader(raw []byte) (gomail.Header, error) {
	e, err := gomessage.Read(bytes.NewReader(raw))
	if e == nil {
		return gomail.Header{}, err
	}
	return gomail.Header{Header: e.Header}, err
}

// partSize computes the decoded size of an attachment part without keeping the
// body in memory.
func partSize(encoding string, body io.Reader) int64 {
	if body == nil {
		return 0
	}
	n, _ := io.Copy(io.Discard, body)
	return n
}

// SetFlags updates seen / flagged for one message.
func (s *Service) SetFlags(ctx context.Context, accountID string, uid uint32, req SetFlagsRequest) error {
	a, err := s.account(accountID)
	if err != nil {
		return err
	}
	mailbox := req.Mailbox
	if mailbox == "" {
		mailbox = "INBOX"
	}
	return s.withConn(ctx, a, func(c *imapclient.Client) error {
		if _, err := c.Select(mailbox, nil).Wait(); err != nil {
			return fmt.Errorf("select %s: %w", mailbox, err)
		}
		set := imap.UIDSetNum(imap.UID(uid))
		if req.Seen != nil {
			if err := storeFlag(c, set, imap.FlagSeen, *req.Seen); err != nil {
				return err
			}
		}
		if req.Flagged != nil {
			if err := storeFlag(c, set, imap.FlagFlagged, *req.Flagged); err != nil {
				return err
			}
		}
		return nil
	})
}

func storeFlag(c *imapclient.Client, set imap.UIDSet, flag imap.Flag, on bool) error {
	op := imap.StoreFlagsAdd
	if !on {
		op = imap.StoreFlagsDel
	}
	_, err := c.Store(set, &imap.StoreFlags{Op: op, Silent: true, Flags: []imap.Flag{flag}}, nil).Collect()
	return err
}

// errNotFound is returned when a mailbox or message does not exist.
var errNotFound = errors.New("not found")
