package transcript

// Runs the parser over a corpus of real transcripts (never committed: they hold real work) and writes what it makes
// of every file, so two versions of the parser can be compared file by file:
//
//	go test -c -o corpus.test ./internal/transcript   (on any version: corpus_shim_test.go adapts the API)
//	./corpus.test -test.run Corpus -corpus ~/persona-corpus/<machine>,~/.claude/projects -out stats.jsonl
//
// Nothing here runs without -corpus.

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

var (
	corpusDirs = flag.String("corpus", "", "comma-separated directories of real transcripts / rollouts")
	corpusOut  = flag.String("out", "corpus.jsonl", "where the per-file results go")
	corpusPerf = flag.Bool("perf", false, "measure cold open time and memory kept instead of item digests")
	corpusMin  = flag.Int64("minsize", 0, "only files at least this many bytes")
)

func corpusFiles(t *testing.T) []string {
	t.Helper()
	if *corpusDirs == "" {
		t.Skip("-corpus")
	}
	var out []string
	for _, d := range strings.Split(*corpusDirs, ",") {
		d = os.ExpandEnv(strings.Replace(d, "~", os.Getenv("HOME"), 1))
		_ = filepath.Walk(d, func(p string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && strings.HasSuffix(p, ".jsonl") && info.Size() >= *corpusMin {
				out = append(out, p)
			}
			return nil
		})
	}
	sort.Strings(out)
	return out
}

func openCorpusFile(p string) *File {
	if strings.Contains(p, "/subagents/") {
		return NewSubagentFile(p)
	}
	return NewFile(p)
}

// What a line is, read independently of the parser: for counting the lines that gave no items.
func lineType(line []byte) string {
	var e struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
		Payload *struct {
			Type string `json:"type"`
			Item *struct {
				Type string `json:"type"`
			} `json:"item"`
		} `json:"payload"`
		Attachment *struct {
			Type string `json:"type"`
		} `json:"attachment"`
	}
	if json.Unmarshal(line, &e) != nil {
		return "(bad json)"
	}
	k := e.Type
	if e.Subtype != "" {
		k += "/" + e.Subtype
	}
	if e.Payload != nil {
		k = "codex " + k + "/" + e.Payload.Type
		if e.Payload.Item != nil {
			k += "/" + e.Payload.Item.Type
		}
	}
	if e.Attachment != nil {
		k += ":" + e.Attachment.Type
	}
	return k
}

var clipNote = regexp.MustCompile(`\n…（已截断(?:，点开看全文)?，共 (\d+) 字节）$`)

// normText takes off a truncation note and gives the text's real length and a hash of its first 24 KB (the most any
// version keeps in a page), so versions that cut at different places compare equal when the text is the same.
func normText(s string) (int, string) {
	n := len(s)
	if m := clipNote.FindStringSubmatchIndex(s); m != nil {
		n, _ = strconv.Atoi(s[m[2]:m[3]])
		s = s[:m[0]]
	}
	if len(s) > 24*1024 {
		cut := 24 * 1024
		for cut > 0 && (s[cut]&0xC0) == 0x80 {
			cut--
		}
		s = s[:cut]
	}
	return n, short(s)
}

func short(s string) string {
	if s == "" {
		return ""
	}
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:6])
}

type corpusRecord struct {
	Path      string          `json:"path"`
	Size      int64           `json:"size"`
	Lines     int             `json:"lines"`
	BadJSON   int             `json:"bad_json"`
	Panic     string          `json:"panic,omitempty"`
	Kinds     map[string]int  `json:"kinds"`
	NoItems   map[string]int  `json:"no_items"` // line type -> lines that gave no items
	Items     [][]any         `json:"items,omitempty"`
	Meta      json.RawMessage `json:"meta,omitempty"`
	OpenMS    float64         `json:"open_ms,omitempty"`
	KeptBytes int64           `json:"kept_bytes,omitempty"`
	FirstPage int             `json:"first_page,omitempty"`
}

func TestCorpus(t *testing.T) {
	files := corpusFiles(t)
	out, err := os.Create(*corpusOut)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	w := bufio.NewWriterSize(out, 1<<20)
	defer w.Flush()
	enc := json.NewEncoder(w)
	start := time.Now()
	for i, p := range files {
		var rec corpusRecord
		if *corpusPerf {
			rec = corpusPerfOne(p)
		} else {
			rec = corpusOne(p)
		}
		if err := enc.Encode(rec); err != nil {
			t.Fatal(err)
		}
		if i%50 == 0 {
			fmt.Fprintf(os.Stderr, "%d/%d files, %v\n", i, len(files), time.Since(start).Round(time.Second))
		}
	}
	fmt.Fprintf(os.Stderr, "done: %d files in %v\n", len(files), time.Since(start).Round(time.Second))
}

func corpusOne(p string) (rec corpusRecord) {
	rec = corpusRecord{Path: p, Kinds: map[string]int{}, NoItems: map[string]int{}}
	defer func() {
		if r := recover(); r != nil {
			rec.Panic = fmt.Sprint(r)
		}
	}()
	st, err := os.Stat(p)
	if err != nil {
		rec.Panic = err.Error()
		return
	}
	rec.Size = st.Size()
	defer shimWhole()()
	f := openCorpusFile(p)
	items, err := f.Refresh()
	if err != nil {
		rec.Panic = err.Error()
		return
	}
	seen := map[int]bool{}
	for _, it := range items {
		seen[it.Seq] = true
		rec.Kinds[it.Role+"/"+it.Kind]++
		full := shimFull(f, it)
		n, h := normText(full.Text)
		img := ""
		if full.Image != nil {
			img = short(full.Image.Mime + full.Image.Data)
		}
		flags := ""
		for _, b := range []struct {
			on bool
			c  string
		}{{it.IsError, "e"}, {it.Queued, "q"}, {it.Wake, "w"}, {it.From != "", "f"}} {
			if b.on {
				flags += b.c
			}
		}
		rec.Items = append(rec.Items, []any{it.ID, it.Seq, it.Role, it.Kind, it.ToolName, it.ToolUseID, flags, n, h, short(string(it.ToolInput)), img})
	}
	// Lines without items, by type (read again, independently of the parser; only the lines it read: a live file
	// grows meanwhile).
	parsed := f.LastSeq()
	fh, err := os.Open(p)
	if err == nil {
		r := bufio.NewReaderSize(fh, 1<<20)
		for n := 1; n <= parsed; n++ {
			line, err := r.ReadBytes('\n')
			if len(line) == 0 || (err != nil && line[len(line)-1] != '\n') {
				break // the last line, not finished: the parser waits for it too
			}
			rec.Lines = n
			lt := lineType(line)
			if lt == "(bad json)" && len(strings.TrimSpace(string(line))) > 0 {
				rec.BadJSON++
			}
			if !seen[n] && len(strings.TrimSpace(string(line))) > 0 {
				rec.NoItems[lt]++
			}
		}
		fh.Close()
	}
	rec.Meta, _ = json.Marshal(f.Meta())
	return rec
}

func heapNow() int64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return int64(m.HeapAlloc)
}

// corpusPerfOne opens the file cold the way the node does for the phone (parse it, take the last two exchanges) and
// measures how long that took and how much memory the open file keeps.
func corpusPerfOne(p string) (rec corpusRecord) {
	rec = corpusRecord{Path: p}
	defer func() {
		if r := recover(); r != nil {
			rec.Panic = fmt.Sprint(r)
		}
	}()
	st, _ := os.Stat(p)
	rec.Size = st.Size()
	before := heapNow()
	t0 := time.Now()
	f := openCorpusFile(p)
	if _, err := f.Refresh(); err != nil {
		rec.Panic = err.Error()
		return
	}
	rec.FirstPage = shimFirstPage(f)
	rec.OpenMS = float64(time.Since(t0).Microseconds()) / 1000
	rec.KeptBytes = heapNow() - before
	rec.Lines = f.LastSeq()
	runtime.KeepAlive(f)
	_ = proto.KindText
	return rec
}
