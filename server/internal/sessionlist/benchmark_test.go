package sessionlist

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func benchmarkIndex(b *testing.B, size int) *Index {
	b.Helper()
	i := &Index{}
	items := make([]Item, size)
	for n := range items {
		id := fmt.Sprintf("session-%05d", n)
		items[n] = testItem(id, n)
		items[n].Machine = fmt.Sprintf("machine-%d", n%4)
		items[n].Full = json.RawMessage(fmt.Sprintf(`{"id":%q,"title":"Demo session","content":%q}`, id, strings.Repeat("demo ", 200)))
		items[n].Summary = json.RawMessage(fmt.Sprintf(`{"id":%q,"title":"Demo session"}`, id))
	}
	i.Replace("batch", items)
	if _, err := i.Query(Options{}); err != nil {
		b.Fatal(err)
	}
	return i
}

// No ordered view or exact-query cache is warmed: every request performs the
// direct lookup anew. Growing the unrelated fleet must not grow request cost.
func BenchmarkExactLookup(b *testing.B) {
	for _, size := range []int{1, 8000, 32000} {
		for _, missing := range []bool{false, true} {
			b.Run(fmt.Sprintf("sessions=%d/missing=%t", size, missing), func(b *testing.B) {
				var i Index
				items := make([]Item, size)
				for n := range items {
					items[n] = testItem(fmt.Sprintf("s%05d", n), n)
				}
				i.Replace("batch", items)
				o := Options{ID: "s00000", Machine: "machine-a"}
				want := 1
				if missing {
					o.ID, want = "missing", 0
				}
				b.ReportAllocs()
				b.ResetTimer()
				for n := 0; n < b.N; n++ {
					p, err := i.Query(o)
					if err != nil || p.Total != want || len(p.Sessions) != want {
						b.Fatalf("exact = %+v, %v", p, err)
					}
				}
			})
		}
	}
}

func BenchmarkWarmQuery(b *testing.B) {
	for _, size := range []int{1000, 8000, 20000} {
		for _, summary := range []bool{false, true} {
			b.Run(fmt.Sprintf("sessions=%d/summary=%t", size, summary), func(b *testing.B) {
				i := benchmarkIndex(b, size)
				o := Options{Summary: summary}
				b.ReportAllocs()
				b.ResetTimer()
				for n := 0; n < b.N; n++ {
					p, err := i.Query(o)
					if err != nil || len(p.Sessions) != defaultLimit {
						b.Fatalf("query = %+v, %v", p, err)
					}
				}
			})
		}
	}
}

func BenchmarkWarmCounts(b *testing.B) {
	i := benchmarkIndex(b, 8000)
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		c, err := i.Counts(Options{})
		if err != nil || c.Total != 8000 {
			b.Fatalf("counts = %+v, %v", c, err)
		}
	}
}

func BenchmarkWarmDelta(b *testing.B) {
	i := benchmarkIndex(b, 8000)
	baseline := i.Revision()
	updated := testItem("changed", 10000)
	updated.Full = json.RawMessage(`{"id":"changed","title":"Changed"}`)
	i.Replace("changes", []Item{updated})
	o := Options{Since: baseline}
	if _, err := i.Query(o); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		p, err := i.Query(o)
		if err != nil || len(p.Sessions) != 1 {
			b.Fatalf("delta = %+v, %v", p, err)
		}
	}
}
