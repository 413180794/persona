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

    def test_storage_member_cannot_collide_with_declared_fields(self):
        for name in ["additional_properties", "additionalProperties", "AdditionalProperties"]:
            with self.subTest(name=name), self.assertRaisesRegex(GEN.SchemaError, "reserved"):
                self.definitions({"Envelope": {
                    "type": "object", "additionalProperties": True,
                    "properties": {name: {"type": "string"}},
                }})


if __name__ == "__main__":
    unittest.main()
