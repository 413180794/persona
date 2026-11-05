package assistant

import "testing"

// A chat item says where it opens in its messaging source (#2138): the connector's id as the source, the account the
// apps pass for a non-default one, the positions to land on; a mail item has none.
func TestInboxItemConversation(t *testing.T) {
	it := &InboxItem{ID: "lark:f2", Source: "lark", Lark: &InboxLark{FeedID: "f2", OLAccount: 2, StartPosition: "201",
		ReadPosition: "199", MentionPosition: "202"}}
	c := it.listed().Conversation
	if c == nil || c.Source != "lark" || c.ID != "f2" || c.Account != "2" || c.StartPosition != "201" || c.ReadPosition != "199" || c.MentionPosition != "202" {
		t.Fatalf("%+v", c)
	}
	it.Lark.OLAccount = 0
	if c := it.listed().Conversation; c.Account != "" {
		t.Fatalf("the default account is no account: %+v", c)
	}
	if c := (&InboxItem{ID: "mail:1", Source: "mail", Mail: &InboxMail{}}).listed().Conversation; c != nil {
		t.Fatalf("a mail: %+v", c)
	}
	if it.Conversation != nil {
		t.Fatal("only the listed copy has it, not what is kept")
	}
}
