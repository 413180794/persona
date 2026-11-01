package node

import (
	"context"
	"encoding/json"
	"time"
)

func (n *Node) notifyCodex() {
	if n.codices == nil {
		return
	}
	select {
	case n.codices.kick <- struct{}{}:
	default:
	}
}

func (n *Node) publishCodex(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-n.codices.kick:
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		for len(n.codices.kick) > 0 {
			<-n.codices.kick
		}
		n.notifySessions()
	}
}

type remoteEventItem struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Text    string `json:"text"`
	Changes []struct {
		Path string `json:"path"`
	} `json:"changes"`
}

func (it *remoteEventItem) UnmarshalJSON(raw []byte) error {
	type plain remoteEventItem
	var envelope struct {
		Payload json.RawMessage `json:"payload"`
		*plain
	}
	envelope.plain = (*plain)(it)
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return err
	}
	if len(envelope.Payload) > 0 && string(envelope.Payload) != "null" {
		return json.Unmarshal(envelope.Payload, (*plain)(it))
	}
	return nil
}
