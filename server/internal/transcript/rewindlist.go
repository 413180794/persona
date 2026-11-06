package transcript

import "github.com/nwverse/persona/server/internal/proto"

// The app names a message to rewind to by its words, which of the messages with those words it is counting from the
// end, and how many of the user's messages came after it (node/rewind.go). It counts every message of the user's it
// shows; Claude Code's /rewind list leaves some of them out (#5397), and the node walks that list.
//
// What Claude Code 2.1.309 lists (its message selector): the user's lines that are not meta, a tool result, a compact
// summary, a command's output, a background task's notice or another agent's message. A local slash command such as
// /rename, which newer versions write as system lines (subtype local_command), is not a user line at all, so it is not
// listed either; the app shows it as the user's message (userText). Older versions wrote the command as a user line,
// which Claude lists, as the app counts it.

// rewindPrompt is one message of the user's as the app counts it: the lines of one transcript entry are one message.
type rewindPrompt struct {
	uuid   string
	words  string // squashed text
	listed bool   // Claude Code's /rewind lists it
}

// rewindPrompts lists the user's messages the way the apps do (web/src/lib/rewind.ts promptsOf): text and pasted
// images of the user's, not queued, one per entry. f.mu is held.
func (f *File) rewindPrompts() []rewindPrompt {
	system := map[string]bool{}
	if f.tree != nil {
		for _, r := range f.tree.recs {
			if r.system {
				system[r.uuid] = true
			}
		}
	}
	var out []rewindPrompt
	var raw []string
	for _, it := range f.items {
		if it.Role != "user" || it.Queued || !(it.Kind == proto.KindText || (it.Kind == proto.KindImage && it.ToolUseID == "")) {
			continue
		}
		uuid := uuidOf(it.ID)
		if n := len(out); n > 0 && out[n-1].uuid == uuid {
			if it.Kind == proto.KindText && it.Text != "" {
				raw[n-1] += "\n" + it.Text
			}
			continue
		}
		text := ""
		if it.Kind == proto.KindText {
			text = it.Text
		}
		out = append(out, rewindPrompt{uuid: uuid, listed: !system[uuid] && it.From == "" && !it.Wake})
		raw = append(raw, text)
	}
	for i := range out {
		out[i].words = squash(raw[i])
	}
	return out
}

// AsListed turns where the app says a message sits (the occurrence-th with these words counting from the end, after
// messages after it) into where it sits in Claude Code's /rewind list. ok is false when the transcript does not have
// the message there, or the message is not one Claude lists: the node then goes by the app's numbers and its check of
// the screen decides.
func (f *File) AsListed(prompt string, occurrence, after int) (listedOccurrence, listedAfter int, ok bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	want := squash(prompt)
	if want == "" || occurrence < 1 || after < 0 {
		return 0, 0, false
	}
	all := f.rewindPrompts()
	at, seen := -1, 0
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].words != want {
			continue
		}
		if seen++; seen == occurrence {
			at = i
			break
		}
	}
	if at < 0 || len(all)-1-at != after || !all[at].listed {
		return 0, 0, false
	}
	listedOccurrence = 1
	for _, p := range all[at+1:] {
		if !p.listed {
			continue
		}
		listedAfter++
		if p.words == want {
			listedOccurrence++
		}
	}
	return listedOccurrence, listedAfter, true
}
