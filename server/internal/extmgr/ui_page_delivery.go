package extmgr

import (
	"encoding/json"
	"sync"
	"sync/atomic"

	"github.com/nwverse/persona/server/internal/extwire"
)

// One bounded FIFO keeps snapshot/delta order without holding session locks over
// the bus writer. A stalled hub cannot stall the extension's source reader.
type pageDelivery struct {
	once     sync.Once
	stopOnce sync.Once
	packets  chan extwire.UITarget
	done     chan struct{}
	bytes    atomic.Int64
}

func (h *Manager) pageEmit(w extwire.UIPrincipal, frame json.RawMessage) bool {
	if h.opts.Hub == nil || w.Connection == "" {
		return true
	}
	q := &h.uiDelivery
	q.once.Do(func() {
		q.packets = make(chan extwire.UITarget, 64)
		q.done = make(chan struct{})
		go func() {
			for {
				select {
				case <-q.done:
					return
				case packet := <-q.packets:
					_ = h.opts.Hub.Notify(extwire.EventUIStream, packet)
					q.bytes.Add(-int64(len(packet.Envelope)))
				}
			}
		}()
	})
	if len(frame) > 1<<20 {
		return false
	}
	if q.bytes.Add(int64(len(frame))) > 4<<20 {
		q.bytes.Add(-int64(len(frame)))
		return false
	}
	select {
	case q.packets <- extwire.UITarget{Who: w, Envelope: frame}:
		return true
	default:
		q.bytes.Add(-int64(len(frame)))
		return false
	}
}
func (h *Manager) pageStopDelivery() {
	q := &h.uiDelivery
	q.once.Do(func() { q.packets = make(chan extwire.UITarget, 64); q.done = make(chan struct{}) })
	q.stopOnce.Do(func() { close(q.done) })
}
