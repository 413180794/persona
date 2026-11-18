package assistant

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/nwverse/persona/server/internal/apierr"

	"github.com/nwverse/persona/server/internal/assistantwire"
)

const feedChannelNotice = "channel_notice"

func (s *Service) noticeMain(ctx context.Context) (string, bool, error) {
	if s.store() == nil {
		return "", false, nil
	}
	v := s.settings(ctx)
	machine := s.confirmedMainMachine(ctx, v)
	if machine == "" || s.isLeaderRef(v.Main) {
		return "", false, nil
	}
	row, ok := s.session(ctx, machine, v.Main)
	if !ok || !row.Assistant || spaceOf(row) != "" {
		return "", false, nil
	}
	_, deleted, err := s.deletedConversation(ctx, machine, v.Main)
	return v.Main, !deleted && err == nil, err
}

// receiveChannelNotice adds a feed row. It never enters transcript, turn routing, or owner-action handling.
func (s *Service) receiveChannelNotice(ctx context.Context, p assistantwire.ChannelNotice) (assistantwire.NoticeResult, error) {
	result := assistantwire.NoticeResult{ID: p.ID}
	if err := p.Notice.Validate(); err != nil {
		return result, err
	}
	st := s.store()
	if st == nil {
		return result, errors.New("passive notices require durable feed storage")
	}
	key := fmt.Sprintf("channel_notice_%x", sha256.Sum256([]byte(p.Extension+"\x00"+p.Channel+"\x00"+p.ID)))
	prior, err := st.FeedItem(ctx, key)
	if err != nil {
		return result, err
	}
	if prior == nil {
		main, ready, err := s.noticeMain(ctx)
		if err != nil {
			return result, err
		}
		if !ready {
			return result, errors.New("main conversation unavailable for passive notices")
		}
		written, err := s.appendFeedOnce(ctx, key, main, feedChannelNotice, "", p)
		if err != nil {
			return result, err
		}
		if written {
			result.Status = "stored"
			return result, nil
		}
		prior, err = st.FeedItem(ctx, key)
		if err != nil {
			return result, err
		}
	}
	var previous assistantwire.ChannelNotice
	if prior == nil || prior.Kind != feedChannelNotice || json.Unmarshal(prior.Body, &previous) != nil || previous != p {
		return result, errors.New("passive notice id already used with different content")
	}
	result.Status = "duplicate"
	return result, nil
}

func (s *Service) channelNoticeRoutes() {
	s.handle("POST /api/v1/assistant/feed/{id}/confirm-channel-notice", func(w http.ResponseWriter, r *http.Request) {
		if !fullRole(r) {
			apierr.Write(w, http.StatusForbidden, errors.New("full credentials required to confirm a reply"))
			return
		}
		ctx := r.Context()
		item, err := s.feedItem(ctx, r.PathValue("id"))
		if err != nil {
			apierr.Call(w, err)
			return
		}
		var p assistantwire.ChannelNotice
		if item == nil || item.Kind != feedChannelNotice || json.Unmarshal(item.Body, &p) != nil || p.Kind != "grounded_reply" {
			apierr.Write(w, http.StatusNotFound, errors.New("no reply draft on this notice"))
			return
		}
		main, ready, err := s.noticeMain(ctx)
		if err != nil {
			apierr.Call(w, err)
			return
		}
		if !ready || item.Conversation != main {
			apierr.Write(w, http.StatusConflict, errors.New("notice is no longer in the main conversation"))
			return
		}
		confirm := assistantwire.ConfirmChannelNotice{Extension: p.Extension, Channel: p.Channel, ID: p.ID}
		if err := s.appCall(ctx, AppChannels, assistantwire.ChannelsConfirmNotice, confirm, nil); err != nil {
			apierr.Write(w, http.StatusBadGateway, errors.New("the extension did not confirm sending this reply"))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
