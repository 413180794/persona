package transcript

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

var cost = flag.Bool("cost", false, "measure opening a ~20 MB transcript (time, memory kept)")

// bigTranscript repeats the redacted session fixture to about 20 MB, its 1×1 screenshots swapped for 300 KB ones
// (what real sessions with screenshots look like).
func bigTranscript(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "claude-session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	png := []byte(`"data":"iVBORw0KGgo`)
	data = bytes.ReplaceAll(data, png, append([]byte(`"data":"`), bytes.Repeat([]byte("QUFB"), 75_000)...))
	var out bytes.Buffer
	for out.Len() < 20<<20 {
		out.Write(data)
	}
	p := filepath.Join(t.TempDir(), "big.jsonl")
	if err := os.WriteFile(p, out.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func heap() uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// go test ./internal/transcript -run ColdOpenCost -v -args -cost
func TestColdOpenCost(t *testing.T) {
	if !*cost {
		t.Skip("-cost")
	}
	p := bigTranscript(t)
	st, _ := os.Stat(p)
	before := heap()
	start := time.Now()
	f := NewFile(p)
	if _, err := f.Refresh(); err != nil {
		t.Fatal(err)
	}
	items, _, _ := f.PageTurns(0, 2, 2000)
	took := time.Since(start)
	kept := heap() - before
	t.Logf("%.1f MB, %d lines: first page (%d items) in %v, %.1f MB kept", float64(st.Size())/(1<<20), f.LastSeq(), len(items), took, float64(kept)/(1<<20))
	runtime.KeepAlive(f)
}
