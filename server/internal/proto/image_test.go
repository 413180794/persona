package proto

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"testing"
)

func TestWithoutImageData(t *testing.T) {
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 30, 20)))
	data := base64.StdEncoding.EncodeToString(buf.Bytes())
	items := []Item{{ID: "a"}, {ID: "b", Image: &Image{Mime: "image/png", Data: data}}}
	out := WithoutImageData(items)
	if got := out[1].Image; got.Data != "" || !got.Lazy || got.Mime != "image/png" || got.Size != len(data)*3/4 || got.Width != 30 || got.Height != 20 {
		t.Fatalf("stripped image = %+v", got)
	}
	if items[1].Image.Data != data {
		t.Fatal("the original items were changed")
	}
	if plain := []Item{{ID: "a"}}; &WithoutImageData(plain)[0] != &plain[0] {
		t.Fatal("items without images should be returned as is")
	}
}
