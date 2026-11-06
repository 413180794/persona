package uistream

import (
	"bytes"
	"testing"
)

func TestSourceSnapshotRejectsNormalizationBeforeFitting(t *testing.T) {
	valid := []byte(`{"generation":"source-generation","version":1e0,"rootKey":"root","view":{"sections":[{"key":"main","items":[{"type":"code","key":"code","text":"","truncated":false}]}]}}`)
	header, err := DecodePageSnapshot(valid)
	if err != nil || header.Version != 1 || header.RootKey != "root" {
		t.Fatalf("valid source snapshot: %+v %v", header, err)
	}
	for _, raw := range [][]byte{
		bytes.Replace(valid, []byte(`"version":1e0`), []byte(`"version":1,"version":2`), 1),
		bytes.Replace(valid, []byte(`"text":""`), []byte(`"text":"\ud800"`), 1),
		bytes.Replace(valid, []byte(`"text":""`), append(append([]byte(`"text":"`), 0xff), []byte(`"`)...), 1),
		bytes.Replace(valid, []byte(`"generation"`), []byte(`"Generation"`), 1),
		bytes.Replace(valid, []byte(`"generation"`), []byte(`"recipient":"another-device","generation"`), 1),
	} {
		if _, err := DecodePageSnapshot(raw); err == nil {
			t.Fatal("source normalization concealed invalid native data")
		}
	}
}
