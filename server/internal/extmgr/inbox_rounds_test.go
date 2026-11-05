package extmgr

import (
	"testing"
)

// Feishu's kicks ask for the chats; a connector's kick (or one meanwhile) still asks for every source.
func TestInboxKickRounds(t *testing.T) {
	h := newTestManager(t)
	h.kickLarkChats()
	<-h.inbox.kick
	if r := h.inbox.kickedRound(); r != roundChats {
		t.Fatalf("Feishu kick: round %d", r)
	}
	h.kickLarkChats()
	h.kickInbox()
	h.kickLarkChats()
	<-h.inbox.kick
	select {
	case <-h.inbox.kick:
		t.Fatal("three kicks in a burst signalled twice")
	default:
	}
	if r := h.inbox.kickedRound(); r != roundPoll {
		t.Fatalf("Feishu and connector kicks: round %d", r)
	}
	h.kickLarkChats()
	<-h.inbox.kick
	if r := h.inbox.kickedRound(); r != roundChats {
		t.Fatalf("after a wide round, a Feishu kick: round %d", r)
	}
}
