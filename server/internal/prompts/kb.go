package prompts

import "time"

// KBOwner is the knowledge base models' system prompts: compressing a chat (kb/compress), rewriting a project
// (kb/project), tagging documents, meetings and events (kb/items).
type KBOwner struct {
	Owner string
}

func RenderKBCompress(o KBOwner) string { return render("kb/compress", o) }
func RenderKBProject(o KBOwner) string  { return render("kb/project", o) }
func RenderKBItems(o KBOwner) string    { return render("kb/items", o) }

// RenderKBReadme is the README.md at the top of the data dir (kb/readme).
func RenderKBReadme() string { return render("kb/readme", nil) }

// KnownProject is a project the models may name (kb/known_projects).
type KnownProject struct {
	Name    string
	Aliases []string
}

// KBCompressInput is a chat's summary so far and its new messages (kb/compress_input).
type KBCompressInput struct {
	Name      string
	Kind      string // dm | group | bot | mail | moment
	Automated bool   // mail from a notification / marketing address
	Projects  []KnownProject
	Tags      []string
	Summary   string
	News      string
}

func RenderKBCompressInput(in KBCompressInput) string { return render("kb/compress_input", in) }

// KBProjectChat is a chat linked to a project, its summary clipped.
type KBProjectChat struct {
	ID, Summary string
}

// KBProjectInput is a project's file, timeline and chats (kb/project_input).
type KBProjectInput struct {
	Name, Kind string
	Aliases    []string
	Tags       []string
	Summary    string
	Log        []string
	Chats      []KBProjectChat
}

func RenderKBProjectInput(in KBProjectInput) string { return render("kb/project_input", in) }

// KBItem is one document, meeting or event to tag.
type KBItem struct {
	Key   string // kind/id
	Kind  string // docs | meetings | events
	Title string
	At    time.Time // local time; zero when unknown
	Where string
}

// KBItemsInput is a batch of items to tag (kb/items_input).
type KBItemsInput struct {
	Projects []KnownProject
	Tags     []string
	Items    []KBItem
}

func RenderKBItemsInput(in KBItemsInput) string { return render("kb/items_input", in) }

// KBDigestEntry is one project or chat in the digest.
type KBDigestEntry struct {
	ID, Name, Importance, OneLine string
	Relevance                     int
}

// KBDigest is the most important projects and chats, for the assistant's live snapshot (kb/digest).
type KBDigest struct {
	Projects, Chats []KBDigestEntry
}

func RenderKBDigest(d KBDigest) string { return render("kb/digest", d) }
