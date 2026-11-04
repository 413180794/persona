package hub

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/lark"
)

const attachmentClientID = "0b6a3c1e-2f4d-4e8a-9c7b-5d1e3f2a4b6c"

func TestLarkAttachmentsBoundAccountAndRetry(t *testing.T) {
	image, _ := base64.StdEncoding.DecodeString("R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==")
	for _, kind := range []string{"images", "files"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/agent/v1/conversations/123/"+kind || r.Header.Get("Authorization") != "Bearer upstream-secret" || r.Header.Get("x-lark-connector-account") != "7" {
					t.Errorf("wrong route or authentication")
				}
				if r.URL.Query().Has("ol_account") {
					t.Error("caller changed the account")
				}
				if kind == "images" {
					var body struct {
						ClientID string `json:"clientId"`
						Data     []byte `json:"data"`
						Name     string `json:"name"`
						MIME     string `json:"mimeType"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ClientID != attachmentClientID || !bytes.Equal(body.Data, image) || body.Name != "图.gif" || body.MIME != "image/gif" {
						t.Errorf("incorrect image request: %v", err)
					}
				} else {
					b, _ := io.ReadAll(r.Body)
					if string(b) != "document" || r.ContentLength != 8 || r.Header.Get("Content-Type") != "application/octet-stream" || r.URL.Query().Get("clientId") != attachmentClientID || r.URL.Query().Get("name") != "文档.txt" {
						t.Error("incorrect file request")
					}
				}
				io.WriteString(w, `{"messageId":"999"}`)
			}))
			defer up.Close()
			h := &Hub{lark: lark.New(lark.Config{BaseURL: up.URL + "/agent/v1", Token: "upstream-secret", Account: 7})}
			mux := http.NewServeMux()
			h.larkAttachmentRoutes(larkMux{mux, h})
			for range 2 {
				var body []byte
				query := url.Values{"ol_account": {"7"}} // names lark.account: the same client, and only the header goes upstream
				if kind == "images" {
					body, _ = json.Marshal(map[string]any{"client_id": attachmentClientID, "name": "图.gif", "mime": "image/gif", "data": image})
				} else {
					body = []byte("document")
					query.Set("client_id", attachmentClientID)
					query.Set("name", "文档.txt")
					query.Set("mime", "text/plain")
				}
				r := httptest.NewRequest("POST", "/api/v1/lark/conversations/123/"+kind+"?"+query.Encode(), bytes.NewReader(body))
				r.Header.Set("Content-Type", "application/octet-stream")
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, r)
				if w.Code != 200 || !strings.Contains(w.Body.String(), `"message_id":"999"`) {
					t.Fatalf("%d %s", w.Code, w.Body.String())
				}
			}
			if calls != 2 {
				t.Fatalf("calls = %d", calls)
			}
		})
	}
}

func TestLarkAttachmentValidationBeforeUpstream(t *testing.T) {
	h := &Hub{} // Invalid requests must not touch the client.
	mux := http.NewServeMux()
	h.larkAttachmentRoutes(larkMux{mux, h})
	path := "/api/v1/lark/conversations/123/files?client_id=" + attachmentClientID + "&name=file.txt&mime=text/plain"
	for _, tc := range []struct {
		path   string
		size   int64
		status int
	}{
		{path, 0, 400}, {path, -1, 411}, {path, lark.FileLimit + 1, 413},
		{strings.Replace(path, "file.txt", "..", 1), 1, 400},
		{strings.Replace(path, attachmentClientID, "wrong", 1), 1, 400},
		{strings.Replace(path, "text/plain", "text", 1), 1, 400},
	} {
		r := httptest.NewRequest("POST", tc.path, strings.NewReader("x"))
		r.ContentLength = tc.size
		r.Header.Set("Content-Type", "application/octet-stream")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Errorf("%s size %d: %d want %d", tc.path, tc.size, w.Code, tc.status)
		}
	}
	for _, body := range []string{`{}`, `{"client_id":"` + attachmentClientID + `","name":"x.jpg","mime":"image/jpeg","data":"bm90IGFuIGltYWdl"}`} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/lark/conversations/123/images", strings.NewReader(body)))
		if w.Code != 400 {
			t.Errorf("invalid image: %d", w.Code)
		}
	}
	for _, kind := range []string{"images", "files"} {
		p := "/api/v1/lark/conversations/123/" + kind
		if allowed(principal{Role: roleAnswer}, "POST", p) {
			t.Error("answer-only device can send an attachment")
		}
		if !allowed(principal{Role: roleFull}, "POST", p) {
			t.Error("full device cannot send")
		}
	}
}

func TestLarkImageLimitAllowsEscapedBase64(t *testing.T) {
	for _, extra := range []int{0, 1} {
		data := bytes.Repeat([]byte{255}, lark.ImageLimit+extra)
		copy(data, []byte("GIF89a"))
		called := false
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			io.WriteString(w, `{"messageId":"image"}`)
		}))
		h := &Hub{lark: lark.New(lark.Config{BaseURL: up.URL, Token: "test"})}
		mux := http.NewServeMux()
		h.larkAttachmentRoutes(larkMux{mux, h})
		body, _ := json.Marshal(map[string]any{"client_id": attachmentClientID, "name": "x.gif", "mime": "image/gif", "data": data})
		body = bytes.ReplaceAll(body, []byte("/"), []byte(`\/`))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/lark/conversations/123/images", bytes.NewReader(body)))
		up.Close()
		want := http.StatusOK
		if extra > 0 {
			want = http.StatusRequestEntityTooLarge
		}
		if w.Code != want || called != (extra == 0) {
			t.Fatalf("extra=%d status=%d called=%v", extra, w.Code, called)
		}
	}
}
