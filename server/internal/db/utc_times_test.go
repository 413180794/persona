package db

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// A hub on a machine east of UTC reads timestamptz in UTC, in both wire formats (#3755).
func TestTimestamptzReadsInUTC(t *testing.T) {
	saved := time.Local
	time.Local = time.FixedZone("UTC+8", 8*3600)
	t.Cleanup(func() { time.Local = saved })

	at := time.Date(2026, 11, 6, 2, 0, 0, 123456000, time.UTC)
	scan := func(m *pgtype.Map, format int16) time.Time {
		t.Helper()
		var src []byte
		if format == pgtype.BinaryFormatCode {
			b, err := m.Encode(pgtype.TimestamptzOID, format, at, nil)
			if err != nil {
				t.Fatal(err)
			}
			src = b
		} else {
			src = []byte("2026-11-06 10:00:00.123456+08") // what Postgres sends with TimeZone = Asia/Shanghai
		}
		var got time.Time
		if err := m.Scan(pgtype.TimestamptzOID, format, src, &got); err != nil {
			t.Fatal(err)
		}
		return got
	}

	// What pgx does on its own: the machine's zone (why the protocol had +08:00).
	if got := scan(pgtype.NewMap(), pgtype.BinaryFormatCode); got.Location() == time.UTC {
		t.Fatalf("default map already reads UTC (%v); the test no longer shows the cause", got)
	}

	m := pgtype.NewMap()
	utcTimes(m)
	for _, format := range []int16{pgtype.BinaryFormatCode, pgtype.TextFormatCode} {
		got := scan(m, format)
		if got.Location() != time.UTC || !got.Equal(at) {
			t.Errorf("format %d: got %v (%v), want %v in UTC", format, got, got.Location(), at)
		}
		if s := got.Format(time.RFC3339Nano); s != "2026-11-06T02:00:00.123456Z" {
			t.Errorf("format %d: RFC3339 %s", format, s)
		}
	}
}
