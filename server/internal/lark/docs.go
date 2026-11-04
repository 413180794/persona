package lark

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"time"
)

// Feishu documents for the inbox (docs/inbox.md「云文档」): the docs notification center (@me, comments, replies),
// reading a sheet / Base / docx around the mention, and the few writes the user asks for from the phone: a status
// cell, a comment.

// Me is the signed-in account: who "@me" is in a document.
type Me struct {
	UserID string `json:"userId"`
	Name   string `json:"name"`
}

func (c *Client) Me(ctx context.Context) (Me, error) {
	var out struct {
		ActiveID string `json:"activeId"`
		Accounts []struct {
			Me
			ID      string `json:"id"`
			Primary bool   `json:"primary"`
			Active  bool   `json:"active"`
		} `json:"accounts"`
	}
	if err := c.do(ctx, "GET", "/accounts", nil, nil, &out); err != nil {
		return Me{}, err
	}
	// The account this client names; the primary one is first in the list whichever is asked about.
	for _, pick := range []func(int) bool{
		func(i int) bool { return out.ActiveID != "" && out.Accounts[i].ID == out.ActiveID },
		func(i int) bool { return out.ActiveID == "" && out.Accounts[i].Active },
		func(i int) bool { return out.ActiveID == "" && out.Accounts[i].Primary },
	} {
		for i := range out.Accounts {
			if pick(i) {
				return out.Accounts[i].Me, nil
			}
		}
	}
	if len(out.Accounts) > 0 {
		return out.Accounts[0].Me, nil
	}
	return Me{}, nil
}

// DocNotice is one card of the docs notification center: a document's latest activity that concerns me.
type DocNotice struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	Type      int       `json:"type"` // drive object type: 2 doc, 3 sheet, 8 Base, 16 wiki, 22 docx
	Kind      string    `json:"kind"` // mention | comment | reply | share | …
	Summary   string    `json:"summary"`
	AtMe      bool      `json:"atMe"`
	Unread    int       `json:"unreadCount"`
	UpdatedAt time.Time `json:"updatedAt"`
	Muted     bool      `json:"muted"`
}

func (c *Client) DocNotices(ctx context.Context) ([]DocNotice, error) {
	var out struct {
		Items []DocNotice `json:"items"`
	}
	err := c.do(ctx, "GET", "/documents/notifications", nil, nil, &out)
	return out.Items, err
}

// DocHeader resolves a link (a wiki node too) to the document behind it.
type DocHeader struct {
	Token string `json:"token"`
	Type  int    `json:"type"`
}

func (c *Client) DocHeader(ctx context.Context, link string) (DocHeader, error) {
	var out DocHeader
	err := c.do(ctx, "GET", "/documents/header", url.Values{"url": {link}}, nil, &out)
	return out, err
}

// DocMarkdown is a docx / doc / sheet rendered as Markdown.
func (c *Client) DocMarkdown(ctx context.Context, link string) (title, markdown string, err error) {
	var out struct {
		Title    string `json:"title"`
		Markdown string `json:"markdown"`
	}
	err = c.do(ctx, "GET", "/documents/read", url.Values{"url": {link}, "format": {"markdown"}}, nil, &out)
	return out.Title, out.Markdown, err
}

// ---- sheets ----

type SheetChoice struct {
	Text  string `json:"text"`
	Color string `json:"color,omitempty"`
}

type SheetCell struct {
	Row     int           `json:"row"`
	Column  int           `json:"column"`
	Text    string        `json:"text"`
	Input   string        `json:"input,omitempty"`
	Formula string        `json:"formula,omitempty"`
	Runs    []TextRun     `json:"runs,omitempty"`
	Choices []SheetChoice `json:"choices,omitempty"` // a list validation: what the cell can be set to
}

// Base is the value a sheet edit names as the cell's current one: formula, input, the runs' text, the text.
func (s SheetCell) Base() string {
	switch {
	case s.Formula != "":
		return s.Formula
	case s.Input != "":
		return s.Input
	case len(s.Runs) > 0:
		t := ""
		for _, r := range s.Runs {
			t += r.Text
		}
		return t
	}
	return s.Text
}

type SheetTab struct {
	ID       string      `json:"id"`
	Name     string      `json:"name"`
	Unloaded bool        `json:"unloaded,omitempty"`
	Cells    []SheetCell `json:"cells"`
}

type Spreadsheet struct {
	Title    string
	Token    string
	URL      string
	Revision int
	Editable bool
	Sheets   []SheetTab
}

// Sheet reads a spreadsheet; tab "" is the default read (tabs past the first may come back unloaded).
func (c *Client) Sheet(ctx context.Context, link, tab string) (Spreadsheet, error) {
	q := url.Values{"url": {link}}
	if tab != "" {
		q.Set("sheet", tab)
	}
	var out struct {
		Title       string `json:"title"`
		Token       string `json:"token"`
		URL         string `json:"url"`
		Spreadsheet struct {
			Sheets   []SheetTab `json:"sheets"`
			Revision int        `json:"revision"`
			Editable bool       `json:"editable"`
		} `json:"spreadsheet"`
	}
	if err := c.do(ctx, "GET", "/documents/read", q, nil, &out); err != nil {
		return Spreadsheet{}, err
	}
	return Spreadsheet{Title: out.Title, Token: out.Token, URL: out.URL, Revision: out.Spreadsheet.Revision,
		Editable: out.Spreadsheet.Editable, Sheets: out.Spreadsheet.Sheets}, nil
}

// SetSheetCell writes one cell, refusing (409) when it no longer holds base.
func (c *Client) SetSheetCell(ctx context.Context, link, sheetID string, revision, row, column int, input, base string) error {
	return c.do(ctx, "POST", "/documents/sheet/edit", nil, map[string]any{"url": link, "sheetId": sheetID, "baseRevision": revision,
		"changes": []map[string]any{{"row": row, "column": column, "input": input, "base": base}}}, nil)
}

// ChooseSheetCell picks a dropdown cell's value (a plain edit refuses cells with dropdown chips).
func (c *Client) ChooseSheetCell(ctx context.Context, link, sheetID string, revision, row, column int, value string) error {
	return c.do(ctx, "POST", "/documents/sheet/structure", nil, map[string]any{"url": link, "sheetId": sheetID, "baseRevision": revision,
		"op": map[string]any{"kind": "choose", "row": row, "column": column, "value": value}}, nil)
}

// ---- Base ----

type TextRun struct {
	Text string `json:"text"`
	Href string `json:"href,omitempty"`
}

type BaseOption struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color int    `json:"color"`
}

type BaseField struct {
	ID      string       `json:"id"`
	Name    string       `json:"name"`
	Type    int          `json:"type"`
	UIType  string       `json:"uiType"`
	Primary bool         `json:"primary"`
	Options []BaseOption `json:"options,omitempty"`
}

// BaseCell is one cell's value: text runs, option ids, user ids, a number, a date (ms), a checkbox…
type BaseCell struct {
	Type    string          `json:"type"`
	Runs    []TextRun       `json:"runs,omitempty"`
	Text    string          `json:"text,omitempty"`
	IDs     []string        `json:"ids,omitempty"`
	Number  *float64        `json:"number,omitempty"`
	Value   json.RawMessage `json:"value,omitempty"`
	Checked *bool           `json:"checked,omitempty"`
	Users   []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"users,omitempty"`
}

// Fingerprint is the cell's current value as a Base edit's `expect` names it (docs-editing.md of lark-connector).
func (b *BaseCell) Fingerprint() string {
	if b == nil {
		return ""
	}
	j := func(v any) string { s, _ := json.Marshal(v); return string(s) }
	switch b.Type {
	case "text":
		var pairs [][2]string
		for _, r := range b.Runs {
			if r.Text != "" || r.Href != "" {
				pairs = append(pairs, [2]string{r.Text, r.Href})
			}
		}
		if len(pairs) == 0 {
			return ""
		}
		return "t:" + j(pairs)
	case "option":
		if len(b.IDs) == 0 {
			return ""
		}
		return "o:" + j(b.IDs)
	case "user", "group":
		if len(b.IDs) == 0 {
			return ""
		}
		return "u:" + j(b.IDs)
	case "checkbox":
		if b.Checked != nil && *b.Checked {
			return "c:1"
		}
	}
	return ""
}

type BaseRecord struct {
	ID    string               `json:"id"`
	Cells map[string]*BaseCell `json:"cells"`
}

type BaseTable struct {
	ID      string       `json:"id"`
	Name    string       `json:"name"`
	Fields  []BaseField  `json:"fields"`
	Records []BaseRecord `json:"records"`
	Next    any          `json:"nextOffset,omitempty"`
	ViewID  string       `json:"viewId,omitempty"`
}

type Base struct {
	Token  string
	URL    string
	Tables []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	Table BaseTable
}

// BaseTable reads one table of a Base (table "" is the first) with its first page of records.
func (c *Client) BaseTable(ctx context.Context, link, table string) (Base, error) {
	q := url.Values{"url": {link}}
	if table != "" {
		q.Set("table", table)
	}
	var out struct {
		Token  string `json:"token"`
		URL    string `json:"url"`
		Tables []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"tables"`
		Table BaseTable `json:"table"`
	}
	if err := c.do(ctx, "GET", "/documents/base/table", q, nil, &out); err != nil {
		return Base{}, err
	}
	return Base{Token: out.Token, URL: out.URL, Tables: out.Tables, Table: out.Table}, nil
}

// BaseRecords is the next page of a table (offset from the previous page's Next).
func (c *Client) BaseRecords(ctx context.Context, link, table string, offset any) ([]BaseRecord, any, error) {
	q := url.Values{"url": {link}, "table": {table}}
	switch o := offset.(type) {
	case string:
		q.Set("offset", o)
	case float64:
		q.Set("offset", strconv.FormatFloat(o, 'f', -1, 64))
	}
	var out struct {
		Records []BaseRecord `json:"records"`
		Next    any          `json:"nextOffset,omitempty"`
	}
	err := c.do(ctx, "GET", "/documents/base/records", q, nil, &out)
	return out.Records, out.Next, err
}

// SetBaseOption sets a single-select cell, refusing (409) when the cell no longer holds what expect says.
func (c *Client) SetBaseOption(ctx context.Context, link, table, record, field, optionID, expect string) error {
	return c.do(ctx, "POST", "/documents/base/edit", nil, map[string]any{"url": link, "edit": map[string]any{
		"action": "set", "table": table, "record": record,
		"cells":  map[string]any{field: map[string]any{"type": "option", "ids": []string{optionID}}},
		"expect": map[string]any{field: expect},
	}}, nil)
}

// RecentDoc is one of the documents the user opened or edited lately (Feishu's 最近), newest activity first.
type RecentDoc struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	URL        string    `json:"url"`
	Type       int       `json:"type"` // drive type: 22 docx, 3 sheet, 8 base, 2 doc…
	ActivityAt time.Time `json:"activityAt"`
}

// RecentDocs is Feishu's 最近 list (documents/recent).
func (c *Client) RecentDocs(ctx context.Context, limit int) ([]RecentDoc, error) {
	var out struct {
		Items []RecentDoc `json:"items"`
	}
	err := c.do(ctx, "GET", "/documents/recent", url.Values{"limit": {strconv.Itoa(limit)}}, nil, &out)
	return out.Items, err
}

// ---- comments ----

type CommentSegment struct {
	Kind string `json:"kind"` // text | mention | link | …
	Text string `json:"text,omitempty"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

type CommentReply struct {
	ID     string `json:"id"`
	Author struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"author"`
	Content   []CommentSegment `json:"content"`
	Mine      bool             `json:"mine,omitempty"`
	CreatedAt time.Time        `json:"createdAt"`
}

type CommentThread struct {
	ID        string         `json:"id"`
	Resolved  bool           `json:"resolved"`
	Whole     bool           `json:"whole"` // on the whole document, not on a selection
	Quote     string         `json:"quote"`
	Replies   []CommentReply `json:"replies"`
	CreatedAt time.Time      `json:"createdAt"`
}

// Comments are a document's comment threads (Base has its own route).
func (c *Client) Comments(ctx context.Context, link string, base bool) ([]CommentThread, error) {
	path := "/documents/comments"
	if base {
		path = "/documents/base/comments"
	}
	var out struct {
		Threads []CommentThread `json:"threads"`
	}
	err := c.do(ctx, "GET", path, url.Values{"url": {link}}, nil, &out)
	return out.Threads, err
}

// ReplyComment answers a comment thread.
func (c *Client) ReplyComment(ctx context.Context, link, threadID, text string) error {
	return c.do(ctx, "POST", "/documents/comments/"+url.PathEscape(threadID)+"/replies", nil, map[string]any{"url": link, "content": text}, nil)
}

// CommentDoc adds a whole-document comment (docx and doc).
func (c *Client) CommentDoc(ctx context.Context, link, text string) error {
	return c.do(ctx, "POST", "/documents/comments", nil, map[string]any{"url": link, "content": text}, nil)
}

// CommentCell comments on a sheet cell (row and column from 0).
func (c *Client) CommentCell(ctx context.Context, link, sheetID string, row, column int, text string) error {
	return c.do(ctx, "POST", "/documents/comments/cell", nil, map[string]any{"url": link, "sheetId": sheetID, "row": row, "column": column, "content": text}, nil)
}

// CommentRecord comments on a Base record.
func (c *Client) CommentRecord(ctx context.Context, link, table, record, text string) error {
	return c.do(ctx, "POST", "/documents/comments/record", nil, map[string]any{"url": link, "table": table, "record": record, "content": text}, nil)
}
