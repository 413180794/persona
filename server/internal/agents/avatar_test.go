package agents

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image/png"
	"net/http"
	"testing"
)

const svgCircle = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><path d="M50 8a42 42 0 1 1 0 84a42 42 0 0 1 0-84z" fill="#FF309B"/></svg>`

func TestAvatarMergedFromFeedAndRasterized(t *testing.T) {
	svgURL := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svgCircle))
	f := &fakeBridge{
		sessions: []AgentSession{{SessionKey: "agent:grok_bot:a:1"}, {SessionKey: "agent:grok_bot:a:2"}},
		avatars:  map[string]string{"agent:grok_bot:a:1": svgURL},
	}
	s := NewService(nil, nil, nil, testBridge{API: f})
	ctx := context.Background()
	list, err := s.Sessions(ctx, "", "")
	if err != nil || len(list) != 2 {
		t.Fatalf("list %v %v", list, err)
	}
	if !list[0].HasAvatar || len(list[0].AvatarVersion) != 12 || list[1].HasAvatar {
		t.Fatalf("avatar flags %+v %+v", list[0], list[1])
	}
	b, _ := json.Marshal(list[0])
	if bytes.Contains(b, []byte("data:")) || !bytes.Contains(b, []byte(`"hasAvatar":true`)) {
		t.Fatalf("list JSON leaks the URL or lacks the flag: %s", b)
	}
	a, err := s.Avatar(ctx, list[0].ID)
	if err != nil || a.ContentType != "image/png" || a.Version != list[0].AvatarVersion {
		t.Fatalf("avatar %v %v", a, err)
	}
	img, err := png.Decode(bytes.NewReader(a.Data))
	if err != nil {
		t.Fatal(err)
	}
	if r, _, _, al := img.At(64, 64).RGBA(); al == 0 || r>>8 != 0xFF {
		t.Fatalf("center pixel not the fill: r=%x a=%x", r>>8, al>>8)
	}
	if _, _, _, al := img.At(1, 1).RGBA(); al != 0 {
		t.Fatalf("corner should be transparent")
	}
	if _, err := s.Avatar(ctx, list[1].ID); HTTPStatus(err) != http.StatusNotFound {
		t.Fatalf("no avatar: %v", err)
	}
}

func TestDecodeAvatarJPEGDataURL(t *testing.T) {
	a, err := decodeAvatar(context.Background(), "data:image/jpeg;base64,"+base64.StdEncoding.EncodeToString([]byte{0xff, 0xd8, 0xff, 0xe0}))
	if err != nil || a.ContentType != "image/jpeg" || len(a.Data) != 4 {
		t.Fatalf("%v %v", a, err)
	}
	if _, err := decodeAvatar(context.Background(), "http://insecure/x.png"); err == nil {
		t.Fatal("plain http accepted")
	}
}
