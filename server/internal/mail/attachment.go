package mail

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	gomail "github.com/emersion/go-message/mail"
)

// attachmentMetadata binds IDs to MIME order, metadata and decoded content.
func attachmentMetadata(h *gomail.AttachmentHeader, body io.Reader, index int, dst io.Writer) (Attachment, error) {
	name, _ := h.Filename()
	ct, _, _ := h.ContentType()
	sum := sha256.New()
	fmt.Fprintf(sum, "%d\x00%s\x00%s\x00", index, name, ct)
	n, err := io.Copy(io.MultiWriter(sum, dst), body)
	return Attachment{ID: strconv.Itoa(index) + "-" + hex.EncodeToString(sum.Sum(nil)), Filename: name, Mime: ct, Size: n}, err
}

func (s *Service) attachment(ctx context.Context, accountID string, uid uint32, mailbox, id string) (*os.File, Attachment, error) {
	a, err := s.account(accountID)
	if err != nil {
		return nil, Attachment{}, err
	}
	indexText, digest, ok := strings.Cut(id, "-")
	index, parseErr := strconv.Atoi(indexText)
	if !ok || parseErr != nil || index < 1 || len(digest) != 64 {
		return nil, Attachment{}, fmt.Errorf("invalid attachment id")
	}
	if mailbox == "" {
		mailbox = "INBOX"
	}
	file, err := os.CreateTemp("", "persona-mail-attachment-*")
	if err != nil {
		return nil, Attachment{}, err
	}
	var metadata Attachment
	err = s.withConn(ctx, a, func(c *imapclient.Client) error {
		if err := file.Truncate(0); err != nil {
			return err
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return err
		}
		if _, err := c.Select(mailbox, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
			return err
		}
		// BODY.PEEK is available in IMAP4rev1; do not assume BINARY or any LIST extension.
		fetch := c.Fetch(imap.UIDSetNum(imap.UID(uid)), &imap.FetchOptions{BodySection: []*imap.FetchItemBodySection{{Peek: true}}})
		defer fetch.Close()
		found := false
		for message := fetch.Next(); message != nil; message = fetch.Next() {
			for item := message.Next(); item != nil; item = message.Next() {
				section, ok := item.(imapclient.FetchItemDataBodySection)
				if !ok || section.Literal == nil {
					continue
				}
				reader, err := gomail.CreateReader(section.Literal)
				if reader == nil {
					return err
				}
				count := 0
				for {
					part, err := reader.NextPart()
					if err == io.EOF {
						break
					}
					if err != nil {
						return err
					}
					header, ok := part.Header.(*gomail.AttachmentHeader)
					if !ok {
						continue
					}
					count++
					if count != index {
						continue
					}
					metadata, err = attachmentMetadata(header, part.Body, count, file)
					if err != nil {
						return err
					}
					if metadata.ID != id {
						return fmt.Errorf("attachment not found")
					}
					found = true
					break
				}
			}
		}
		if err := fetch.Close(); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("attachment not found")
		}
		return nil
	})
	if err != nil {
		file.Close()
		os.Remove(file.Name())
		return nil, Attachment{}, err
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		os.Remove(file.Name())
		return nil, Attachment{}, err
	}
	return file, metadata, nil
}

func (s *Service) handleAttachment(w http.ResponseWriter, r *http.Request) {
	uid, err := strconv.ParseUint(r.PathValue("uid"), 10, 32)
	if err != nil || uid == 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid uid"))
		return
	}
	file, metadata, err := s.attachment(r.Context(), r.PathValue("aid"), uint32(uid), r.URL.Query().Get("mailbox"), r.PathValue("attachment"))
	if err != nil {
		s.fail(w, err)
		return
	}
	defer os.Remove(file.Name())
	defer file.Close()
	ct := metadata.Mime
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": metadata.Filename}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("ETag", `"`+metadata.ID+`"`)
	// ServeContent handles Range, If-Range, Content-Range and 416 over decoded bytes.
	http.ServeContent(w, r, metadata.Filename, time.Time{}, file)
}
