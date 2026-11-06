"""The wire generator retains explicit additionalProperties on any object, not just Contributes (#2467)."""

import importlib.util
import json
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location("extension_codegen_under_test", ROOT / "scripts/gen/extension_schema.py")
GEN = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = GEN
SPEC.loader.exec_module(GEN)


class ExtensionCodegenTests(unittest.TestCase):
    def definitions(self, schemas):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "example.schema.json"
            path.write_text(json.dumps({"$defs": schemas}))
            with patch.object(GEN, "SCHEMA_DIR", Path(tmp)), patch.object(GEN, "SCHEMA_FILES", [path.name]):
                return GEN.load()

    def test_explicit_open_objects_keep_known_types_and_extra_keys(self):
        props = {"name": {"type": "string"}, "enabled": {"type": "boolean"}}
        definitions = self.definitions({
            "Envelope": {"type": "object", "properties": props, "required": ["name"], "additionalProperties": True},
            "TypedExtras": {"type": "object", "properties": props, "additionalProperties": {"type": "string"}},
            "EmptyEnvelope": {"type": "object", "additionalProperties": True},
            "ClosedEnvelope": {"type": "object", "properties": props, "additionalProperties": False},
            "UnspecifiedEnvelope": {"type": "object", "properties": props},
        })
        for name in ["Envelope", "TypedExtras", "EmptyEnvelope"]:
            self.assertTrue(definitions[name].extra)
        for name in ["ClosedEnvelope", "UnspecifiedEnvelope"]:
            self.assertFalse(definitions[name].extra)
        go = GEN.gofmt(GEN.gen_go(definitions))
        self.assertIn("func (v *Envelope) UnmarshalJSON", go)
        self.assertIn("func (v EmptyEnvelope) MarshalJSON", go)
        self.assertIn("map[string]json.RawMessage", go)
        self.assertIn('`json:"name"`', go)
        ts = GEN.gen_ts(definitions)
        self.assertIn("name: string;", ts)
        self.assertEqual(ts.count("[key: string]: unknown;"), 3)
        swift = GEN.gen_swift(definitions)
        self.assertIn("struct ExtensionEnvelope:", swift)
        self.assertIn("var name: String", swift)
        self.assertIn("var additionalProperties: [String: JSONValue]", swift)
        kotlin = GEN.gen_kotlin(definitions)
        self.assertIn("object ExtensionEnvelopeSerializer", kotlin)
        self.assertIn("val name: String", kotlin)
        self.assertIn("val additionalProperties: Map<String, JsonElement>", kotlin)

    def test_stream_extensions_are_generic(self):
        definitions = self.definitions({
            "Native": {"type": "object", "properties": {"key": {"type": "string"}}},
            "Keyed": {"$ref": "#/$defs/Native", "required": ["key"]},
            "Value": {"oneOf": [{"type": "null"}, {"type": "boolean"}, {"type": "string"}],
                      "x-untagged": True, "x-json-value": True},
            "Replace": {"type": "object", "properties": {"op": {"const": "replace"},
                        "left": {"$ref": "#/$defs/Keyed"}, "right": {"$ref": "#/$defs/Native"}},
                        "required": ["op"], "oneOf": [{"required": ["left"]}, {"required": ["right"]}]},
            "Send": {"type": "object", "properties": {"op": {"const": "send"}, "protocol": {"type": "integer", "const": 1},
                     "values": {"type": "array", "items": {"$ref": "#/$defs/Value"}}},
                     "required": ["op", "protocol", "values"]},
            "Operation": {"oneOf": [{"$ref": "#/$defs/Replace"}, {"$ref": "#/$defs/Send"}],
                          "x-discriminator": "op", "x-preserve-required": True, "x-unknown-policy": "recover"},
        })
        self.assertNotIn("Keyed", definitions)
        self.assertEqual(definitions["Replace"].fields[1].type.ref, "Native")
        ts = GEN.gen_ts(definitions)
        self.assertIn("export type Value = null | boolean | string;", ts)
        self.assertIn("left: Native; right?: never", ts)
        self.assertIn("protocol: 1;", ts)
        go = GEN.gofmt(GEN.gen_go(definitions))
        self.assertIn("func (v Operation) MarshalJSON", go)
        self.assertRegex(go, r'Values\s+\[\]Value `json:"values"`')
        self.assertNotIn('var fields map[string]json.RawMessage', go)
        kotlin = GEN.gen_kotlin(definitions)
        self.assertIn("object ExtensionSendSerializer", kotlin)
        self.assertIn("Json(output.json) { encodeDefaults = true; explicitNulls = false }", kotlin)
        swift = GEN.gen_swift(definitions)
        self.assertIn("self.protocol = try c.decode(Int.self, forKey: .`protocol`)", swift)
        self.assertIn("self.left = c.contains(.left) ? try c.decode(ExtensionNative.self, forKey: .left) : nil", swift)
        # Existing native models remain permissive; only new stream envelopes inherit preservation/strictness.
        self.assertIn("self.key = try? c.decodeIfPresent(String.self, forKey: .key)", swift)
        for output in [swift, kotlin]:
            self.assertIn("request a full snapshot", output)

    def test_stream_native_wrappers_leave_legacy_encoding_unchanged(self):
        definitions = self.definitions({
            "Item": {"type": "object", "properties": {"type": {"const": "code"}, "text": {"type": "string"}},
                     "required": ["type", "text"]},
            "Native": {"oneOf": [{"$ref": "#/$defs/Item"}], "x-discriminator": "type"},
            "Tree": {"type": "object", "properties": {"items": {"type": "array", "items": {"$ref": "#/$defs/Native"}}},
                     "required": ["items"]},
            "Snapshot": {"type": "object", "properties": {"tree": {"$ref": "#/$defs/Tree"}},
                         "required": ["tree"], "x-preserve-required": True},
        })
        go = GEN.gofmt(GEN.gen_go(definitions))
        self.assertNotIn("func (v Native) MarshalJSON", go)
        self.assertNotIn("func (v Tree) MarshalJSON", go)
        self.assertIn("func (v Snapshot) MarshalJSON", go)
        self.assertIn("func (v requiredJSONNative) MarshalJSON", go)
        self.assertIn("func (v requiredJSONTree) MarshalJSON", go)
        self.assertIn('Text string `json:"text"`', go)
        self.assertIn("[]requiredJSONNative", go)
        self.assertNotIn("var fields map[string]json.RawMessage", go)

    def test_typed_json_dependencies_respect_application_boundary(self):
        definitions = self.definitions({
            "Private": {"type": "object", "properties": {"value": {"type": "string"}}},
            "Value": {"oneOf": [{"type": "array", "items": {"$ref": "#/$defs/Private"}}],
                      "x-untagged": True, "x-json-value": True},
        })
        definitions["Private"].side = "plugin"
        with self.assertRaisesRegex(GEN.SchemaError, "Value refers to Private"):
            GEN.for_apps(definitions)

    def test_validation_alias_cycles_are_rejected(self):
        with self.assertRaisesRegex(GEN.SchemaError, "cyclic validation alias"):
            self.definitions({"First": {"$ref": "#/$defs/Second"}, "Second": {"$ref": "#/$defs/First"}})

    def test_invalid_payload_choice_is_not_guessed(self):
        with self.assertRaisesRegex(GEN.SchemaError, "single required payload"):
            self.definitions({"Bad": {"type": "object", "properties": {"payload": {"type": "string"}},
                                      "oneOf": [{"required": ["payload"], "type": "object"}]}})

    def test_storage_member_cannot_collide_with_declared_fields(self):
        for name in ["additional_properties", "additionalProperties", "AdditionalProperties"]:
            with self.subTest(name=name), self.assertRaisesRegex(GEN.SchemaError, "reserved"):
                self.definitions({"Envelope": {
                    "type": "object", "additionalProperties": True,
                    "properties": {name: {"type": "string"}},
                }})


if __name__ == "__main__":
    unittest.main()
