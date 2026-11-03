package lark

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAttachmentCancellationStopsUpstream(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
		close(stopped)
	}))
	defer up.Close()
	c := New(Config{BaseURL: up.URL, Token: "test"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := c.SendFile(ctx, "123", NewClientID(), "file.txt", "text/plain", strings.NewReader("test"), 4)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("upload did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("upload did not cancel")
	}
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream kept running")
	}
}

func TestAttachmentPreservesUpstreamFailures(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		want   string
	}{
		{502, `{"error":{"code":"UPLOAD_FAILED","message":"retry later"}}`, "UPLOAD_FAILED"},
		{401, `{"error":{"code":"AUTH_SESSION_EXPIRED","message":"login expired"}}`, "未登录飞书"},
	} {
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); io.WriteString(w, tc.body) }))
		c := New(Config{BaseURL: up.URL, Token: "test"})
		_, err := c.SendImage(context.Background(), "123", NewClientID(), "photo.jpg", "image/jpeg", []byte("test"))
		up.Close()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%d: %v", tc.status, err)
		}
	}
}

func TestAcceptedFileWithoutMessageIDIsStillSent(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"fileKey":"test-file","name":"file.txt","size":4}`)
	}))
	defer up.Close()
	c := New(Config{BaseURL: up.URL, Token: "test"})
	receipt, err := c.SendFile(context.Background(), "123", NewClientID(), "file.txt", "text/plain", strings.NewReader("test"), 4)
	if err != nil || receipt.MessageID != "" {
		t.Fatalf("accepted file: %+v, %v", receipt, err)
	}
}

func TestAttachmentTimeoutScalesWithFileSize(t *testing.T) {
	for _, size := range []int64{1, FileLimit} {
		c := New(Config{BaseURL: "http://example.invalid", Token: "test"})
		c.media = &http.Client{Transport: attachmentDeadlineTransport(func(req *http.Request) (*http.Response, error) {
			deadline, ok := req.Context().Deadline()
			left := time.Until(deadline)
			minimum := 2 * time.Minute
			if size == FileLimit {
				minimum = 13 * time.Minute
			}
			if !ok || left < minimum || left > 15*time.Minute {
				t.Errorf("size %d: deadline %s, present %v", size, left, ok)
			}
			return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
		})}
		if _, err := c.SendFile(context.Background(), "123", NewClientID(), "file.txt", "text/plain", strings.NewReader("x"), size); err != nil {
			t.Fatal(err)
		}
	}
}

type attachmentDeadlineTransport func(*http.Request) (*http.Response, error)

func (f attachmentDeadlineTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
