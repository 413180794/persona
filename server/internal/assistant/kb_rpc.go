package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/bus"
	"github.com/nwverse/persona/server/internal/kb"
	"github.com/nwverse/persona/server/internal/rpc"
)

// kbDecodeParams is strict so a misspelled update cannot silently succeed without applying the intended change.
func kbDecodeParams(method string, raw json.RawMessage, out any) error {
	if len(raw) > 1<<20 || !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		return kbInvalidParams(method, "expected an object of at most 1 MiB")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return kbInvalidParams(method, err.Error())
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return kbInvalidParams(method, "expected one object")
	}
	return nil
}

func kbInvalidParams(method, message string) error {
	return &rpc.Error{Code: bus.CodeInvalidParams, Message: method + ": " + message}
}

func kbRPCKind(kind string) bool {
	switch kind {
	case kb.KindProject, kb.KindChat, kb.KindPerson, kb.KindDoc, kb.KindMeeting, kb.KindEvent:
		return true
	}
	return false
}

func kbRPCError(err error) error {
	if errors.Is(err, kb.ErrNotFound) {
		return &rpc.Error{Code: "not_found", Message: err.Error()}
	}
	return kbError(err)
}

// kbRPCRequest shares the engine with the apps and MCP tools, without invoking an HTTP handler or exposing agents.
func (s *Service) kbRPCRequest(ctx context.Context, method string, raw json.RawMessage) (any, bool, error) {
	var list assistantwire.KBListParams
	var get assistantwire.KBGetParams
	var day assistantwire.KBDayParams
	var search assistantwire.KBSearchParams
	var update assistantwire.KBUpdateParams
	var params any
	switch method {
	case assistantwire.MethodKBList:
		params = &list
	case assistantwire.MethodKBGet:
		params = &get
	case assistantwire.MethodKBDay:
		params = &day
	case assistantwire.MethodKBSearch:
		params = &search
	case assistantwire.MethodKBUpdate:
		params = &update
	default:
		return nil, false, nil
	}
	if err := kbDecodeParams(method, raw, params); err != nil {
		return nil, true, err
	}
	invalid := func(message string) (any, bool, error) { return nil, true, kbInvalidParams(method, message) }
	switch method {
	case assistantwire.MethodKBList:
		if !kbRPCKind(list.Kind) || list.Limit < 0 || list.Limit > 200 || list.Offset < 0 {
			return invalid("kind must name an entity kind; limit must be between 0 and 200; offset must be nonnegative")
		}
		if list.Limit == 0 {
			list.Limit = 30
		}
	case assistantwire.MethodKBGet:
		if !kbRPCKind(get.Kind) || strings.TrimSpace(get.ID) == "" {
			return invalid("kind and id are required")
		}
		if get.RecentChars == nil {
			n := 12000
			get.RecentChars = &n
		}
		if *get.RecentChars < 0 || *get.RecentChars > 20000 {
			return invalid("recent_chars must be between 0 and 20000")
		}
	case assistantwire.MethodKBDay:
		if strings.TrimSpace(day.ID) == "" {
			return invalid("id is required")
		}
		if _, err := time.Parse("2006-01-02", day.Day); err != nil {
			return invalid("day must be YYYY-MM-DD")
		}
	case assistantwire.MethodKBSearch:
		if strings.TrimSpace(search.Query) == "" || search.Limit < 0 || search.Limit > 200 {
			return invalid("query is required; limit must be between 0 and 200")
		}
		if search.Limit == 0 {
			search.Limit = 40
		}
	case assistantwire.MethodKBUpdate:
		if !kbRPCKind(update.Kind) || strings.TrimSpace(update.ID) == "" {
			return invalid("kind and id are required")
		}
	}
	e, err := s.kbGet(ctx)
	if err != nil {
		return nil, true, kbRPCError(err)
	}
	switch method {
	case assistantwire.MethodKBList:
		all := e.List(list.Kind, "")
		items := all
		if list.Tag != "" {
			items = e.List(list.Kind, list.Tag)
		}
		if items == nil {
			items = []kb.Entry{}
		}
		total := len(items)
		start := min(list.Offset, total)
		end := start + min(total-start, list.Limit)
		items = items[start:end]
		var next *int
		if end < total {
			next = &end
		}
		tags := kb.KindTags(all, 30)
		if tags == nil {
			tags = []string{}
		}
		return assistantwire.KBListResult{Items: items, Tags: tags, Total: total, NextOffset: next}, true, nil
	case assistantwire.MethodKBGet:
		detail, err := e.Get(get.Kind, get.ID, *get.RecentChars)
		return assistantwire.KBGetResult{Detail: detail}, true, kbRPCError(err)
	case assistantwire.MethodKBDay:
		messages, prev, next, err := e.Day(day.ID, day.Day)
		if messages == nil {
			messages = []kb.DayMessage{}
		}
		return assistantwire.KBDayResult{Day: day.Day, Messages: messages, Prev: prev, Next: next}, true, kbRPCError(err)
	case assistantwire.MethodKBSearch:
		hits := e.Search(search.Query, search.Messages == nil || *search.Messages, search.Limit)
		if hits == nil {
			hits = []kb.Hit{}
		}
		return assistantwire.KBSearchResult{Hits: hits}, true, nil
	default:
		id, err := e.Apply(update.Kind, update.ID, update.Update)
		return assistantwire.KBUpdateResult{ID: id}, true, kbRPCError(err)
	}
}
