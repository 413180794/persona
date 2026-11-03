package hub

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/nwverse/persona/server/internal/lark"
)

var larkFeedID = regexp.MustCompile(`^[0-9]{1,20}$`)

func validAttachmentName(s string) bool {
	return strings.TrimSpace(s) != "" && s != "." && s != ".." && len(utf16.Encode([]rune(s))) <= 255 &&
		!strings.ContainsAny(s, `/\\`) && strings.IndexFunc(s, unicode.IsControl) < 0
}

func validAttachmentMIME(s string) bool {
	t, params, err := mime.ParseMediaType(s)
	return err == nil && strings.Contains(t, "/") && len(params) == 0 && len(s) <= 200
}

func (h *Hub) larkAttachmentRoutes(api *http.ServeMux) {
	api.HandleFunc("POST /api/v1/lark/conversations/{id}/images", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ClientID string `json:"client_id"`
			Name     string `json:"name"`
			MIME     string `json:"mime"`
			Data     []byte `json:"data"`
		}
		// JSON permits every base64 slash to be escaped; the decoded byte limit below is unchanged.
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 20<<20+4096))
		if err := dec.Decode(&in); err != nil {
			var large *http.MaxBytesError
			if errors.As(err, &large) {
				writeErr(w, http.StatusRequestEntityTooLarge, errors.New("图片不能超过 7.5 MiB"))
				return
			}
			writeErr(w, http.StatusBadRequest, errors.New("图片数据无效"))
			return
		}
		if !larkFeedID.MatchString(r.PathValue("id")) || !lark.ValidClientID(in.ClientID) || !validAttachmentName(in.Name) ||
			!validAttachmentMIME(in.MIME) || !strings.HasPrefix(in.MIME, "image/") || len(in.Data) == 0 || dec.Decode(new(any)) != io.EOF {
			writeErr(w, http.StatusBadRequest, errors.New("图片信息无效"))
			return
		}
		if len(in.Data) > lark.ImageLimit {
			writeErr(w, http.StatusRequestEntityTooLarge, errors.New("图片不能超过 7.5 MiB"))
			return
		}
		// Check the bytes too: a file renamed .jpg is not an image.
		if actual := http.DetectContentType(in.Data); !strings.HasPrefix(actual, "image/") || actual != in.MIME {
			writeErr(w, http.StatusBadRequest, errors.New("图片格式不匹配"))
			return
		}
		out, err := h.lark.SendImage(r.Context(), r.PathValue("id"), in.ClientID, in.Name, in.MIME, in.Data)
		if err != nil {
			writeLarkErr(w, err)
			return
		}
		writeJSON(w, out)
	})
	api.HandleFunc("POST /api/v1/lark/conversations/{id}/files", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if !larkFeedID.MatchString(r.PathValue("id")) || !lark.ValidClientID(q.Get("client_id")) ||
			!validAttachmentName(q.Get("name")) || !validAttachmentMIME(q.Get("mime")) || r.Header.Get("Content-Type") != "application/octet-stream" {
			writeErr(w, http.StatusBadRequest, errors.New("文件信息无效"))
			return
		}
		if r.ContentLength < 0 {
			writeErr(w, http.StatusLengthRequired, errors.New("缺少文件大小"))
			return
		}
		if r.ContentLength == 0 {
			writeErr(w, http.StatusBadRequest, errors.New("不能发送空文件"))
			return
		}
		if r.ContentLength > lark.FileLimit {
			writeErr(w, http.StatusRequestEntityTooLarge, errors.New("文件不能超过 100 MiB"))
			return
		}
		out, err := h.lark.SendFile(r.Context(), r.PathValue("id"), q.Get("client_id"), q.Get("name"), q.Get("mime"),
			http.MaxBytesReader(w, r.Body, r.ContentLength), r.ContentLength)
		if err != nil {
			writeLarkErr(w, err)
			return
		}
		writeJSON(w, out)
	})
}
