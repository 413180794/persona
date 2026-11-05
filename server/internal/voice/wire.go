package voice

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// A wire is one dialect of the GPT-Live protocol: the frames to send, and what the service's frames mean.
type wire interface {
	url(base, model string) (string, error)
	header() http.Header
	start(s session) any
	audio(pcm []byte) any
	// text is a line the user typed; say is something the model should speak about now (a delegated task came back
	// after its delegation was gone).
	text(s string) any
	say(s string) any
	// reply answers a delegation: final is the result the model speaks about, otherwise quiet progress.
	reply(delegation, s string, final bool) []any
	// toolResult answers a function call (live dialect with tools).
	toolResult(delegation, callID, output string) []any
	close() any
	decode(raw []byte) []event
	// outputRate is the rate of the service's audio when session.started does not say.
	outputRate(input int) int
}

// session is what a conversation starts the service with.
type session struct {
	Model, Voice, Instructions string
	InputRate                  int
	Tools                      []toolDecl
}

type toolDecl struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type evKind int

const (
	evStarted     evKind = iota // the session is up; rate: its output audio rate (0: the wire's default)
	evAudio                     // audio: PCM16 to play
	evUser                      // text of what the user said (replace: the whole line so far; final: it is done)
	evAssistant                 // text of what the model says
	evTurnEnd                   // the model's turn is over
	evInterrupted               // the model's answer was cut off: drop what has not been played
	evDelegation                // the model hands work over: id, text (what to do, may be empty)
	evToolCall                  // a function call: id (its delegation), callID, name, args
	evError                     // text, code
)

type event struct {
	kind             evKind
	text             string
	replace, final   bool
	audio            []byte
	id, callID, name string
	args, code       string
	rate             int
}

func newWire(dialect string) wire {
	if dialect == DialectCodex {
		return codexWire{}
	}
	return liveWire{}
}

// liveWire is the public GPT-Live API (also served, with function tools as an extension, by deployments that speak
// it for their own models).
type liveWire struct{}

func (liveWire) url(base, _ string) (string, error) { return base, nil }
func (liveWire) header() http.Header                { return http.Header{} }

func (liveWire) start(s session) any {
	audio := map[string]any{"format": map[string]any{"type": "audio/pcm", "rate": s.InputRate}}
	if s.Voice != "" {
		audio["output"] = map[string]any{"voice": s.Voice}
	}
	sess := map[string]any{"model": s.Model, "audio": audio, "delegation": map[string]any{"type": "client"}}
	if s.Instructions != "" {
		sess["instructions"] = s.Instructions
	}
	if len(s.Tools) > 0 {
		sess["tools"] = s.Tools
	}
	return map[string]any{"type": "session.start", "session": sess}
}

func (liveWire) audio(pcm []byte) any {
	return map[string]any{"type": "session.input_audio.append", "audio": base64.StdEncoding.EncodeToString(pcm)}
}

// A commentary without a delegation is a turn of the user's (typed input).
func (liveWire) text(s string) any {
	return map[string]any{"type": "session.commentary.append", "delegation_id": nil, "content": clipTokens(s, appendTokens)}
}
func (w liveWire) say(s string) any { return w.text(s) }

func (liveWire) reply(delegation, s string, final bool) []any {
	typ := "session.thinking.append"
	if final {
		typ = "session.commentary.append"
	}
	return []any{map[string]any{"type": typ, "delegation_id": delegation, "content": clipTokens(s, appendTokens)}}
}

func (liveWire) toolResult(delegation, _, output string) []any {
	return []any{map[string]any{"type": "session.commentary.append", "delegation_id": delegation, "content": clipTokens(output, appendTokens)}}
}

func (liveWire) close() any            { return map[string]any{"type": "session.close"} }
func (liveWire) outputRate(in int) int { return in }

func (liveWire) decode(raw []byte) []event {
	var f struct {
		Type    string `json:"type"`
		Delta   string `json:"delta"`
		Session struct {
			Audio struct {
				Format struct {
					Rate int `json:"rate"`
				} `json:"format"`
				Output struct {
					Format struct {
						Rate int `json:"rate"`
					} `json:"format"`
				} `json:"output"`
			} `json:"audio"`
		} `json:"session"`
		Delegation struct {
			ID      string        `json:"id"`
			Target  string        `json:"target"`
			Content []contentPart `json:"content"`
		} `json:"delegation"`
		DelegationID string          `json:"delegation_id"`
		Event        json.RawMessage `json:"event"`
		Error        struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &f) != nil {
		return nil
	}
	switch f.Type {
	case "session.started":
		rate := f.Session.Audio.Output.Format.Rate
		if rate == 0 {
			rate = f.Session.Audio.Format.Rate
		}
		return []event{{kind: evStarted, rate: rate}}
	case "session.output_audio.delta":
		pcm, err := base64.StdEncoding.DecodeString(f.Delta)
		if err != nil || len(pcm) == 0 {
			return nil
		}
		return []event{{kind: evAudio, audio: pcm}}
	case "session.input_transcript.delta":
		return []event{{kind: evUser, text: f.Delta}}
	case "session.output_transcript.delta":
		return []event{{kind: evAssistant, text: f.Delta, name: "transcript"}}
	case "session.delegation.created":
		// Only a client delegation is work for us; "responses" ones carry the service's own response lifecycle.
		if f.Delegation.Target != "" && f.Delegation.Target != "client" {
			return nil
		}
		return []event{{kind: evDelegation, id: f.Delegation.ID, text: inputText(f.Delegation.Content)}}
	case "response.event":
		return liveResponseEvent(f.DelegationID, f.Event)
	case "error":
		return []event{{kind: evError, text: f.Error.Message, code: f.Error.Code}}
	}
	return nil
}

// liveResponseEvent reads the Responses event a response.event carries: a function call (bare, or as the finished
// item of response.output_item.done), the model's text, the end of its turn.
func liveResponseEvent(delegation string, raw json.RawMessage) []event {
	var e struct {
		Type   string `json:"type"`
		Delta  string `json:"delta"`
		CallID string `json:"call_id"`
		Name   string `json:"name"`
		Args   string `json:"arguments"`
		Item   struct {
			Type   string `json:"type"`
			CallID string `json:"call_id"`
			Name   string `json:"name"`
			Args   string `json:"arguments"`
		} `json:"item"`
	}
	if json.Unmarshal(raw, &e) != nil {
		return nil
	}
	switch e.Type {
	case "function_call":
		return []event{{kind: evToolCall, id: delegation, callID: e.CallID, name: e.Name, args: e.Args}}
	case "response.output_item.done":
		if e.Item.Type == "function_call" {
			return []event{{kind: evToolCall, id: delegation, callID: e.Item.CallID, name: e.Item.Name, args: e.Item.Args}}
		}
	case "response.output_text.delta":
		return []event{{kind: evAssistant, text: e.Delta, name: "text"}}
	case "response.completed", "response.failed":
		return []event{{kind: evTurnEnd}}
	case "response.incomplete", "response.cancelled":
		return []event{{kind: evInterrupted}}
	}
	return nil
}

// codexWire is what Codex speaks to gpt-live-1-codex (codex-rs codex-api realtime_websocket, "frameless bidi").
type codexWire struct{}

func (codexWire) url(base, model string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	if model != "" {
		q := u.Query()
		q.Set("model", model)
		u.RawQuery = q.Encode()
	}
	return u.String(), nil
}

func (codexWire) header() http.Header { return http.Header{"Openai-Alpha": {"quicksilver=v2"}} }

func (codexWire) start(s session) any {
	sess := map[string]any{"instructions": s.Instructions, "delegation": map[string]any{"type": "client"}}
	if s.Voice != "" {
		sess["audio"] = map[string]any{"output": map[string]any{"voice": s.Voice}}
	}
	return map[string]any{"type": "session.update", "session": sess}
}

func (codexWire) audio(pcm []byte) any {
	return map[string]any{"type": "input_audio.append", "audio": base64.StdEncoding.EncodeToString(pcm)}
}

func (w codexWire) text(s string) any { return w.say(s) }

// Context for the whole session; speakable is what the model talks about (an omitted channel means the same).
func (codexWire) say(s string) any {
	return map[string]any{"type": "session.context.append", "channel": "speakable", "content": inputContent(clipBytes(s, codexAppendBytes))}
}

func (codexWire) reply(delegation, s string, final bool) []any {
	channel := "commentary"
	if final {
		channel = "speakable"
	}
	var out []any
	for _, chunk := range chunkBytes(clipTokens(s, 4*appendTokens), codexAppendBytes) {
		out = append(out, map[string]any{
			"type": "delegation.context.append", "delegation_item_id": delegation, "channel": channel, "content": inputContent(chunk),
		})
	}
	return out
}

func (w codexWire) toolResult(delegation, _, output string) []any {
	return w.reply(delegation, output, true)
}
func (codexWire) close() any         { return map[string]any{"type": "session.close"} }
func (codexWire) outputRate(int) int { return 24000 }

func (codexWire) decode(raw []byte) []event {
	var f struct {
		Type  string `json:"type"`
		Audio string `json:"audio"`
		Item  struct {
			ID      string        `json:"id"`
			Type    string        `json:"type"`
			Target  string        `json:"target"`
			Text    string        `json:"text"`
			Content []contentPart `json:"content"`
		} `json:"item"`
		Turn struct {
			Role       string `json:"role"`
			Transcript string `json:"transcript"`
		} `json:"turn"`
		Message string          `json:"message"`
		Error   json.RawMessage `json:"error"`
	}
	if json.Unmarshal(raw, &f) != nil {
		return nil
	}
	switch f.Type {
	case "session.started":
		return []event{{kind: evStarted}}
	case "output_audio.delta":
		pcm, err := base64.StdEncoding.DecodeString(f.Audio)
		if err != nil || len(pcm) == 0 {
			return nil
		}
		return []event{{kind: evAudio, audio: pcm}}
	case "input_transcript.added":
		return []event{{kind: evUser, text: f.Item.Text}}
	case "output_transcript.added":
		return []event{{kind: evAssistant, text: f.Item.Text}}
	case "turn.done":
		switch f.Turn.Role {
		case "user":
			return []event{{kind: evUser, text: f.Turn.Transcript, replace: true, final: true}}
		case "assistant":
			return []event{{kind: evAssistant, text: f.Turn.Transcript, replace: true, final: true}, {kind: evTurnEnd}}
		}
	case "delegation.created":
		if f.Item.Type != "delegation" || f.Item.Target != "client" || f.Item.ID == "" {
			return nil
		}
		return []event{{kind: evDelegation, id: f.Item.ID, text: inputText(f.Item.Content)}}
	case "error":
		msg := f.Message
		if msg == "" {
			var e struct {
				Message string `json:"message"`
				Code    string `json:"code"`
			}
			if json.Unmarshal(f.Error, &e) == nil && e.Message != "" {
				return []event{{kind: evError, text: e.Message, code: e.Code}}
			}
			msg = strings.TrimSpace(string(f.Error))
		}
		return []event{{kind: evError, text: msg}}
	}
	return nil
}

type contentPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func inputText(parts []contentPart) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Type == "input_text" {
			b.WriteString(p.Text)
		}
	}
	return strings.TrimSpace(b.String())
}

func inputContent(s string) []contentPart { return []contentPart{{Type: "input_text", Text: s}} }

const (
	appendTokens     = 450 // GPT-Live: an append's content is limited to 500 tokens
	codexAppendBytes = 500 // Codex splits its context appends at 500 bytes
)

// clipTokens keeps the start of s within about n tokens (a CJK character is about one, other text about four
// characters to one).
func clipTokens(s string, n int) string {
	budget := float64(n)
	for i, r := range s {
		if r >= 0x2E80 {
			budget--
		} else {
			budget -= 0.25
		}
		if budget < 0 {
			return strings.TrimSpace(s[:i]) + "…"
		}
	}
	return s
}

func clipRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func clipBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return chunkBytes(s, n)[0]
}

// chunkBytes splits s into pieces of at most n bytes, at character boundaries.
func chunkBytes(s string, n int) []string {
	var out []string
	for len(s) > n {
		end := n
		for end > 0 && !utf8RuneStart(s[end]) {
			end--
		}
		out = append(out, s[:end])
		s = s[end:]
	}
	return append(out, s)
}

func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }
