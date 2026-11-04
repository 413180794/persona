package plugin

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const formSchema = `{"type": "object", "properties": {
  "url": {"type": "string", "title": "地址", "format": "uri"},
  "mode": {"type": "string", "oneOf": [{"const": "fast", "title": "快"}, {"const": "safe", "title": "稳"}], "default": "safe"},
  "count": {"type": "integer", "minimum": 1, "maximum": 10, "default": 3},
  "ratio": {"type": "number", "minimum": 0, "maximum": 1},
  "on": {"type": "boolean"},
  "mail": {"type": "string", "format": "email"},
  "host": {"type": "string", "format": "hostname"},
  "code": {"type": "string", "pattern": "^[A-Z]{3}$", "minLength": 3, "maxLength": 3},
  "tags": {"type": "array", "items": {"type": "string", "enum": ["a", "b", "c"]}, "uniqueItems": true, "maxItems": 2},
  "proxy": {"type": "object", "properties": {"host": {"type": "string"}, "port": {"type": "integer", "default": 8080}}, "required": ["host"]}
}, "required": ["url"]}`

func TestSchemaOrderAndForm(t *testing.T) {
	s, err := ParseSchema(json.RawMessage(formSchema))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range s.Fields {
		names = append(names, f.Name)
	}
	if strings.Join(names, ",") != "url,mode,count,ratio,on,mail,host,code,tags,proxy" {
		t.Fatal("the order written:", names)
	}
	if !s.Fields[0].Required || s.Fields[1].Required {
		t.Fatal("required")
	}
	if len(s.Fields[1].Enum) != 2 || s.Fields[1].Enum[0].Title != "快" {
		t.Fatalf("%+v", s.Fields[1].Enum)
	}
	if s.Fields[9].Fields[0].Name != "host" || !s.Fields[9].Fields[0].Required {
		t.Fatalf("%+v", s.Fields[9])
	}
	b, _ := json.Marshal(s)
	if strings.Contains(string(b), "pattern\":null") || !strings.Contains(string(b), `"items":{"name":"items","type":"string"`) {
		t.Fatal(string(b))
	}
}

func TestSchemaChecks(t *testing.T) {
	s, err := ParseSchema(json.RawMessage(formSchema))
	if err != nil {
		t.Fatal(err)
	}
	ok := []string{
		`{"url": "https://example.com"}`,
		`{"url": "https://example.com", "mode": "fast", "count": 10, "ratio": 0.5, "on": true, "mail": "me@example.com",
		  "host": "example.com", "code": "ABC", "tags": ["a", "c"], "proxy": {"host": "p.example.com"}}`,
		`{"url": "https://example.com", "mode": null}`,
	}
	for _, v := range ok {
		if err := s.Check(json.RawMessage(v)); err != nil {
			t.Errorf("%s: %v", v, err)
		}
	}
	bad := map[string]string{
		`{}`:                               "url",
		`{"url": "nope"}`:                  "url",
		`{"url": "https://e.com", "x": 1}`: "x",
		`{"url": "https://e.com", "mode": "slow"}`:                      "mode",
		`{"url": "https://e.com", "count": 1.5}`:                        "count",
		`{"url": "https://e.com", "count": 11}`:                         "count",
		`{"url": "https://e.com", "ratio": "1"}`:                        "ratio",
		`{"url": "https://e.com", "on": "yes"}`:                         "on",
		`{"url": "https://e.com", "mail": "Me <me@example.com>"}`:       "mail",
		`{"url": "https://e.com", "host": "a b"}`:                       "host",
		`{"url": "https://e.com", "code": "abc"}`:                       "code",
		`{"url": "https://e.com", "tags": ["a", "a"]}`:                  "tags",
		`{"url": "https://e.com", "tags": ["d"]}`:                       "tags",
		`{"url": "https://e.com", "tags": ["a","b","c"]}`:               "tags",
		`{"url": "https://e.com", "proxy": {}}`:                         "proxy.host",
		`{"url": "https://e.com", "proxy": {"host": "h", "user": "u"}}`: "proxy.user",
		`[]`: "",
	}
	for v, path := range bad {
		err := s.Check(json.RawMessage(v))
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Path != path {
			t.Errorf("%s: %v (want at %q)", v, err, path)
		}
	}
	var none *Schema
	if none.Check(json.RawMessage(`{}`)) != nil || none.Check(nil) != nil || none.Check(json.RawMessage(`{"a":1}`)) == nil {
		t.Fatal("no schema: only an empty object")
	}
}

func TestSchemaEffective(t *testing.T) {
	s, _ := ParseSchema(json.RawMessage(formSchema))
	var got map[string]any
	_ = json.Unmarshal(s.Effective(json.RawMessage(`{"url": "https://e.com", "proxy": {"host": "h"}}`)), &got)
	if got["mode"] != "safe" || got["count"] != float64(3) || got["proxy"].(map[string]any)["port"] != float64(8080) {
		t.Fatal(got)
	}
	if _, has := got["ratio"]; has {
		t.Fatal("no default, no value")
	}
}

func TestSchemaRefusesOutsideTheSubset(t *testing.T) {
	for _, bad := range []string{
		`[]`,
		`{"type": "array"}`,
		`{"type": "object", "properties": {}, "additionalProperties": true}`,
		`{"type": "object", "properties": {"a": {"type": "string", "$ref": "#/x"}}}`,
		`{"type": "object", "properties": {"a": {"anyOf": []}}}`,
		`{"type": "object", "properties": {"a": {"type": "null"}}}`,
		`{"type": "object", "properties": {"a": {"type": "string", "minimum": 1}}}`,
		`{"type": "object", "properties": {"a": {"type": "array", "items": {"type": "object", "properties": {}}}}}`,
		`{"type": "object", "properties": {"a": {"type": "object", "properties": {"b": {"type": "object", "properties": {}}}}}}`,
		`{"type": "object", "properties": {"a": {"type": "string", "format": "date"}}}`,
		`{"type": "object", "properties": {"a": {"type": "string", "pattern": "("}}}`,
		`{"type": "object", "properties": {"a": {"type": "string", "enum": [1]}}}`,
		`{"type": "object", "properties": {"a": {"type": "string", "default": 1}}}`,
		`{"type": "object", "properties": {"a": {"type": "string", "oneOf": [{"const": "x", "if": {}}]}}}`,
		`{"type": "object", "properties": {"a": {"type": "string"}}, "required": ["b"]}`,
		`{"type": "object", "properties": {"a": {"type": "string"}, "a": {"type": "string"}}}`,
		`{"type": "object", "properties": {"a b": {"type": "string"}}}`,
	} {
		if _, err := ParseSchema(json.RawMessage(bad)); err == nil {
			t.Errorf("%s parsed", bad)
		}
	}
}
