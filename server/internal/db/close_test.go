package db

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestCloseContextWithBorrowedConnection(t *testing.T) {
	url := os.Getenv("PERSONA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("PERSONA_TEST_DATABASE_URL not set")
	}
	d, err := Open(context.Background(), url, AllSets)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := d.pool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if err := d.CloseContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close with a borrowed connection: %v", err)
	}
	conn.Release()
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := d.CloseContext(ctx); err != nil {
		t.Fatalf("close after returning connection: %v", err)
	}
}
