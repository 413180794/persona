package assistant

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/proto"
)

const (
	larkContextExtension = "lark-context"
	feedLarkMessage      = "lark_message"
)

var (
	larkMessageID = regexp.MustCompile(`^[0-9]{1,64}$`)
	larkChatID    = regexp.MustCompile(`^(?:[0-9]{1,64}|oc_[0-9a-fA-F]{32})$`)
	larkAvatarRef = regexp.MustCompile(`^avatar:[0-9]{1,64}:[0-9a-f]{64}$`)
)

type larkFeedMessage struct {
	ID     string              `json:"id"`
	At     time.Time           `json:"at"`
	Text   string              `json:"text"`
	Author proto.MessageAuthor `json:"author"`
}

func (m larkFeedMessage) valid() bool {
	a := m.Author
	return larkMessageID.MatchString(m.ID) && !m.At.IsZero() &&
		strings.TrimSpace(m.Text) != "" && len(m.Text) <= 16000 &&
		larkMessageID.MatchString(a.ID) && strings.TrimSpace(a.Name) != "" && len(a.Name) <= 256 &&
		(a.Avatar == "" || larkAvatarRef.MatchString(a.Avatar) && strings.HasPrefix(a.Avatar, "avatar:"+a.ID+":")) &&
		a.Source.Kind == proto.SourceLark && larkChatID.MatchString(a.Source.ConversationID) &&
		len(a.Source.Title) <= 512 && a.Source.Push == "" && (!a.OnBehalf || a.Owner)
}

func (s *Service) larkMessageRoutes() {
	s.handle("POST /api/v1/assistant/conversations/{id}/lark-messages", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(RoleHeader) != roleExtension || requestOpener(r) != larkContextExtension {
			apierr.Write(w, http.StatusForbidden, errors.New("Lark Context credentials required"))
			return
		}
		id := r.PathValue("id")
		var in struct {
			MachineID string            `json:"machine_id"`
			Messages  []larkFeedMessage `json:"messages"`
		}
		if !validSessionID(id) || decodeBody(w, r, &in, 2<<20) != nil ||
			!validSessionID(in.MachineID) || len(in.Messages) == 0 || len(in.Messages) > 16 {
			apierr.Write(w, http.StatusBadRequest, errors.New("invalid Lark conversation messages"))
			return
		}
		for _, m := range in.Messages {
			if !m.valid() {
				apierr.Write(w, http.StatusBadRequest, errors.New("invalid Lark message or author"))
				return
			}
		}

		ctx := r.Context()
		if s.store() == nil {
			apierr.Write(w, http.StatusServiceUnavailable, errors.New("Lark messages require durable feed storage"))
			return
		}
		if err := s.conversationAvailable(ctx, in.MachineID, id); err != nil {
			apierr.Call(w, err)
			return
		}
		if !s.ownsLarkConversation(ctx, in.MachineID, id) {
			apierr.Write(w, http.StatusNotFound, errors.New("no Lark Context conversation"))
			return
		}
		for _, m := range in.Messages {
			key := fmt.Sprintf("lark_message_%x", sha256.Sum256([]byte(id+"\x00"+m.Author.Source.ConversationID+"\x00"+m.ID)))
			if _, _, err := s.appendFeedItem(ctx, key, true, id, feedLarkMessage, "", m, time.Now()); err != nil {
				apierr.Call(w, err)
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// The assistant owns conversation provenance. Node session rows do not carry the
// extension identity and may be unavailable while the machine is offline.
func (s *Service) ownsLarkConversation(ctx context.Context, machine, id string) bool {
	b := &s.origins
	b.mu.Lock()
	defer b.mu.Unlock()
	if !s.originsLocked(ctx) {
		return false
	}
	o := b.m[id]
	return o != nil && o.By == originByStart && o.MachineID == machine &&
		o.Origin == OriginLarkAgent && o.Ref == larkContextExtension && o.Ext == larkContextExtension
}
