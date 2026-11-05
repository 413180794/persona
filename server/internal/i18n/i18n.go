// Package i18n is the hub's text for people in the user's language (docs/i18n.md): pushes, task notifications,
// injected messages, status and error text. Logs, CLI output and prompts for models are not translated here.
//
// Catalogs are locales/<lang>/<namespace>.json, flat "name": "text", embedded in the binary; a key is
// "<namespace>.<name>". Placeholders are {name}; a numeric "count" picks <key>_one / <key>_other when those exist.
// A key missing in a language falls back to en, then zh-Hans, then the key itself (the catalog test keeps them equal).
package i18n

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Lang is one of the three interface languages.
type Lang string

const (
	ZhHans Lang = "zh-Hans"
	En     Lang = "en"
	Ja     Lang = "ja"
)

// Default is the language when no client has said one: the hub's text was Chinese before i18n.
const Default = ZhHans

// Langs lists the supported languages, the source language first.
var Langs = []Lang{ZhHans, En, Ja}

// Name is the language by its own name (what a prompt asks the model to answer in).
func (l Lang) Name() string {
	switch l {
	case En:
		return "English"
	case Ja:
		return "日本語"
	}
	return "简体中文"
}

// Resolve maps any BCP 47 tag to a supported language (docs/i18n.md): any Chinese, Traditional too, is zh-Hans, any
// Japanese is ja, the rest is en. An empty tag is "" (unknown), so callers can tell "not said" from "said English".
func Resolve(tag string) Lang {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return ""
	}
	primary, _, _ := strings.Cut(strings.ReplaceAll(tag, "_", "-"), "-")
	switch strings.ToLower(primary) {
	case "zh":
		return ZhHans
	case "ja":
		return Ja
	}
	return En
}

// Valid: one of the three codes exactly (what a client sends).
func Valid(l Lang) bool {
	return l == ZhHans || l == En || l == Ja
}

// FromAcceptLanguage resolves an Accept-Language header: its first entry (an app sends just "en"; a browser its whole
// list, most preferred first). "" when there is none or it is "*".
func FromAcceptLanguage(h string) Lang {
	first, _, _ := strings.Cut(h, ",")
	first, _, _ = strings.Cut(first, ";")
	first = strings.TrimSpace(first)
	if first == "" || first == "*" {
		return ""
	}
	return Resolve(first)
}

// FromRequest is the request's Accept-Language, resolved ("" when absent).
func FromRequest(r *http.Request) Lang {
	return FromAcceptLanguage(r.Header.Get("Accept-Language"))
}

type ctxKey struct{}

// With carries a language in a context (the request's, set by the hub's HTTP middleware).
func With(ctx context.Context, l Lang) context.Context {
	if l == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, l)
}

// From is the context's language, "" when none was set.
func From(ctx context.Context) Lang {
	l, _ := ctx.Value(ctxKey{}).(Lang)
	return l
}

// Or is l, or fallback when l is unknown.
func (l Lang) Or(fallback Lang) Lang {
	if Valid(l) {
		return l
	}
	return fallback
}

//go:embed locales
var files embed.FS

var catalogs = load()

func load() map[Lang]map[string]string {
	out := map[Lang]map[string]string{}
	for _, l := range Langs {
		out[l] = map[string]string{}
		entries, err := fs.ReadDir(files, "locales/"+string(l))
		if err != nil {
			panic(fmt.Sprintf("i18n: %s: %v", l, err))
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			raw, err := files.ReadFile(path.Join("locales", string(l), e.Name()))
			if err != nil {
				panic(err)
			}
			var m map[string]string
			if err := json.Unmarshal(raw, &m); err != nil {
				panic(fmt.Sprintf("i18n: %s/%s: %v", l, e.Name(), err))
			}
			ns := strings.TrimSuffix(e.Name(), ".json")
			for k, v := range m {
				out[l][ns+"."+k] = v
			}
		}
	}
	return out
}

// Args are a text's placeholder values.
type Args map[string]any

var placeholder = regexp.MustCompile(`\{(\w+)\}`)

func lookup(l Lang, key string, args Args) (string, bool) {
	cat := catalogs[l]
	if n, ok := count(args); ok {
		form := "other"
		if l == En && n == 1 {
			form = "one"
		}
		if s, ok := cat[key+"_"+form]; ok {
			return s, true
		}
		if s, ok := cat[key+"_other"]; ok {
			return s, true
		}
	}
	s, ok := cat[key]
	return s, ok
}

func count(args Args) (int64, bool) {
	switch v := args["count"].(type) {
	case int:
		return int64(v), true
	case int64:
		return v, true
	case int32:
		return int64(v), true
	case float64:
		return int64(v), v == float64(int64(v))
	}
	return 0, false
}

// T is key's text in l (an unknown l is the default), placeholders filled from args.
func T(l Lang, key string, args ...Args) string {
	var a Args
	if len(args) > 0 {
		a = args[0]
	}
	l = l.Or(Default)
	s, ok := lookup(l, key, a)
	if !ok {
		if s, ok = lookup(En, key, a); !ok {
			if s, ok = lookup(ZhHans, key, a); !ok {
				return key
			}
		}
	}
	if a == nil {
		return s
	}
	return placeholder.ReplaceAllStringFunc(s, func(m string) string {
		v, ok := a[m[1:len(m)-1]]
		if !ok {
			return m
		}
		switch v := v.(type) {
		case string:
			return v
		case int:
			return strconv.Itoa(v)
		case int64:
			return strconv.FormatInt(v, 10)
		}
		return fmt.Sprint(v)
	})
}

// Has: key is in the catalogs (any plural form counts).
func Has(key string) bool {
	cat := catalogs[ZhHans]
	_, a := cat[key]
	_, b := cat[key+"_other"]
	return a || b
}

// Message is an error whose text is a catalog entry: written to a client (apierr) it reads in that client's
// language; anywhere else (logs, older paths) Error() is the default language's text, what it said before i18n.
type Message struct {
	Key  string
	Args Args
}

// Err is an error with key's text.
func Err(key string, args ...Args) error {
	m := &Message{Key: key}
	if len(args) > 0 {
		m.Args = args[0]
	}
	return m
}

func (m *Message) Error() string { return T(Default, m.Key, m.Args) }

// Text is the message in l.
func (m *Message) Text(l Lang) string { return T(l, m.Key, m.Args) }

// Localize is err's text in l: a Message (not wrapped in more text) reads in l, any other error as it is.
func Localize(err error, l Lang) string {
	var m *Message
	if l != "" && errors.As(err, &m) && err.Error() == m.Error() {
		return m.Text(l)
	}
	return err.Error()
}

// Carrier is a ResponseWriter that knows its request's language (the hub's request logger sets it).
type Carrier interface{ Lang() Lang }

// OfWriter is the language of the request w answers: the first writer in its Unwrap chain that carries one.
func OfWriter(w http.ResponseWriter) Lang {
	for range 16 {
		if c, ok := w.(Carrier); ok {
			return c.Lang()
		}
		u, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return ""
		}
		w = u.Unwrap()
	}
	return ""
}
