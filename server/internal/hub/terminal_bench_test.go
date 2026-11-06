package hub

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// BenchmarkTerminalFrameFanOut relays one node frame (a ~4 KB base64 screen) to 1, 2 and 3 watchers of a terminal.
func BenchmarkTerminalFrameFanOut(b *testing.B) {
	data := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("\x1b[1;32mok\x1b[0m line of output\r\n", 120)))
	params, err := json.Marshal(map[string]any{"session_id": "s1", "epoch": "e1", "seq": 7, "full": true, "cols": 120, "rows": 40, "data": data})
	if err != nil {
		b.Fatal(err)
	}
	for _, n := range []int{1, 2, 3} {
		b.Run("subs="+strconv.Itoa(n), func(b *testing.B) {
			h := &Hub{}
			subs := map[*client]map[string]bool{}
			var clients []*client
			for range n {
				c := &client{out: make(chan []byte, 1)}
				subs[c] = map[string]bool{"m1": true}
				clients = append(clients, c)
			}
			h.terms.relays = map[[2]string]*termRelay{{"m1", "s1"}: {subs: subs, epoch: "e1"}}
			b.ReportAllocs()
			for b.Loop() {
				h.onTerminalFrame("m1", params)
				for _, c := range clients {
					<-c.out
				}
			}
		})
	}
}
