package lark

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const ImageLimit = 15 << 19 // lark-connector accepts at most 10 MiB of base64 (7.5 MiB decoded).
const FileLimit = 100 << 20

type AttachmentReceipt struct {
	MessageID string `json:"message_id"`
}

// SendImage and SendFile use the configured account and preserve the caller's idempotency key.
// These are dedicated writes; the generic raw proxy remains read-only.
func (c *Client) SendImage(ctx context.Context, feed, clientID, name, mime string, data []byte) (AttachmentReceipt, error) {
	b, err := json.Marshal(map[string]any{"clientId": clientID, "name": name, "mimeType": mime, "data": data})
	if err != nil {
		return AttachmentReceipt{}, err
	}
	return c.sendAttachment(ctx, "/conversations/"+url.PathEscape(feed)+"/images", nil, "application/json", bytes.NewReader(b), int64(len(b)))
}

func (c *Client) SendFile(ctx context.Context, feed, clientID, name, mime string, body io.Reader, size int64) (AttachmentReceipt, error) {
	q := url.Values{"clientId": {clientID}, "name": {name}, "mime": {mime}}
	return c.sendAttachment(ctx, "/conversations/"+url.PathEscape(feed)+"/files", q, "application/octet-stream", body, size)
}

func (c *Client) sendAttachment(ctx context.Context, path string, q url.Values, contentType string, body io.Reader, size int64) (AttachmentReceipt, error) {
	token, err := c.agentToken(ctx)
	if err != nil {
		return AttachmentReceipt{}, err
	}
	u := strings.TrimRight(c.cfg.BaseURL, "/") + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	ctx, cancel := context.WithTimeout(ctx, attachmentTimeout(size))
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, body)
	if err != nil {
		return AttachmentReceipt{}, err
	}
	c.authorize(req, token)
	req.ContentLength = size
	req.Header.Set("Content-Type", contentType)
	resp, err := c.media.Do(req)
	if err != nil {
		return AttachmentReceipt{}, fmt.Errorf("lark: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return AttachmentReceipt{}, err
	}
	if resp.StatusCode/100 != 2 {
		if resp.StatusCode == http.StatusUnauthorized {
			return AttachmentReceipt{}, c.unauthorized(raw)
		}
		code, msg := upstreamError(raw)
		return AttachmentReceipt{}, &doError{Status: resp.StatusCode, Code: code, Message: msg}
	}
	var out struct {
		MessageID string `json:"messageId"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return AttachmentReceipt{}, err
	}
	return AttachmentReceipt{MessageID: out.MessageID}, nil
}

// Include the phone-to-hub stream at 128 KiB/s plus response time, bounded by the upload size limit.
func attachmentTimeout(size int64) time.Duration {
	return max(3*time.Minute, time.Duration(min(max(size, 0), FileLimit)/(128<<10))*time.Second+time.Minute)
}
