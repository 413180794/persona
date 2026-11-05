"""schema/extension (#1849): the generated wire types match the schemas, and the examples hold to them.

    python3 scripts/test/test_extension_schema.py

Each schema/extension/examples/<Definition>.<name>.json must validate against that definition, each file under
examples/invalid/ must not. The validator below knows the subset the generator does (README「子集」) and nothing more.
"""

import json
import re
import subprocess
import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SCHEMA_DIR = ROOT / "schema" / "extension"
GENERATOR = ROOT / "scripts" / "gen" / "extension_schema.py"


def load_schemas():
    files = {}
    for f in sorted(SCHEMA_DIR.glob("*.schema.json")):
        files[f.name] = json.loads(f.read_text())
    return files


class Validator:
    def __init__(self, files):
        self.files = files

    def resolve(self, ref, here):
        name, _, pointer = ref.partition("#")
        doc = self.files[name or here]
        node = doc
        for part in pointer.strip("/").split("/"):
            if part:
                node = node[part]
        return node, (name or here)

    def errors(self, schema, value, here, at="$"):
        if "$ref" in schema:
            target, f = self.resolve(schema["$ref"], here)
            errs = self.errors(target, value, f, at)
            rest = {k: v for k, v in schema.items() if k not in ("$ref", "description")}
            return errs + (self.errors(rest, value, here, at) if rest else [])
        errs = []
        if "oneOf" in schema:
            disc = schema.get("x-discriminator")
            if disc:
                tag = value.get(disc) if isinstance(value, dict) else None
                for option in schema["oneOf"]:
                    target, f = self.resolve(option["$ref"], here)
                    if target["properties"][disc].get("const") == tag:
                        return self.errors(target, value, f, at)
                return [f"{at}: no variant with {disc} {tag!r}"]
            passing = [o for o in schema["oneOf"] if not self.errors(o, value, here, at)]
            if len(passing) != 1:
                errs.append(f"{at}: matches {len(passing)} of oneOf, not one")
            return errs
        t = schema.get("type")
        types = {"string": str, "boolean": bool, "object": dict, "array": list}
        if t == "integer":
            ok = isinstance(value, int) and not isinstance(value, bool)
        elif t == "number":
            ok = isinstance(value, (int, float)) and not isinstance(value, bool)
        elif t:
            ok = isinstance(value, types[t])
        else:
            ok = True
        if not ok:
            return [f"{at}: not a {t}"]
        if "const" in schema and value != schema["const"]:
            errs.append(f"{at}: not {schema['const']!r}")
        if "enum" in schema and value not in schema["enum"]:
            errs.append(f"{at}: {value!r} not one of {schema['enum']}")
        if isinstance(value, str):
            if len(value) < schema.get("minLength", 0):
                errs.append(f"{at}: shorter than {schema['minLength']}")
            if "maxLength" in schema and len(value) > schema["maxLength"]:
                errs.append(f"{at}: longer than {schema['maxLength']}")
            if "pattern" in schema and not re.search(schema["pattern"], value):
                errs.append(f"{at}: {value!r} does not match {schema['pattern']}")
        if isinstance(value, (int, float)) and not isinstance(value, bool):
            if "minimum" in schema and value < schema["minimum"]:
                errs.append(f"{at}: below {schema['minimum']}")
            if "maximum" in schema and value > schema["maximum"]:
                errs.append(f"{at}: above {schema['maximum']}")
        if isinstance(value, list):
            if len(value) < schema.get("minItems", 0):
                errs.append(f"{at}: fewer than {schema['minItems']} items")
            if "maxItems" in schema and len(value) > schema["maxItems"]:
                errs.append(f"{at}: more than {schema['maxItems']} items")
            for i, v in enumerate(value):
                errs += self.errors(schema.get("items", {}), v, here, f"{at}[{i}]")
        if isinstance(value, dict):
            props = schema.get("properties", {})
            for k in schema.get("required", []):
                if k not in value:
                    errs.append(f"{at}: no {k}")
            if len(value) < schema.get("minProperties", 0):
                errs.append(f"{at}: fewer than {schema['minProperties']} properties")
            extra = schema.get("additionalProperties", True)
            for k, v in value.items():
                if k in props:
                    errs += self.errors(props[k], v, here, f"{at}.{k}")
                elif extra is False:
                    errs.append(f"{at}: unknown field {k}")
                elif isinstance(extra, dict):
                    errs += self.errors(extra, v, here, f"{at}.{k}")
        return errs

    def validate(self, definition, value):
        for name, doc in self.files.items():
            if definition in doc.get("$defs", {}):
                return self.errors(doc["$defs"][definition], value, name)
        raise KeyError(definition)


class ExtensionSchemaTests(unittest.TestCase):
    def setUp(self):
        self.v = Validator(load_schemas())

    def test_generated_files_are_current(self):
        r = subprocess.run([sys.executable, str(GENERATOR), "--check"], capture_output=True, text=True)
        self.assertEqual(r.returncode, 0, r.stderr)

    def test_open_object_codegen_is_generic(self):
        r = subprocess.run([sys.executable, str(ROOT / "scripts/test/test_extension_codegen.py")],
                           capture_output=True, text=True, timeout=60)
        self.assertEqual(r.returncode, 0, r.stderr)

    def test_examples_hold_to_the_schema(self):
        examples = sorted((SCHEMA_DIR / "examples").glob("*.json"))
        self.assertGreater(len(examples), 10)
        for f in examples:
            with self.subTest(example=f.name):
                errs = self.v.validate(f.name.split(".")[0], json.loads(f.read_text()))
                self.assertEqual(errs, [])

    def test_invalid_examples_are_refused(self):
        examples = sorted((SCHEMA_DIR / "examples" / "invalid").glob("*.json"))
        self.assertGreater(len(examples), 5)
        for f in examples:
            with self.subTest(example=f.name):
                self.assertNotEqual(self.v.validate(f.name.split(".")[0], json.loads(f.read_text())), [])

    def test_persona_card_cases_hold_to_the_schema(self):
        # examples/persona-card/<case>.json: a persona-card and the view every app turns it into (null: not drawn,
        # it stays a code block); sdk/go/wire/personacard.go is the reference (TestPersonaCardCases).
        cases = sorted((SCHEMA_DIR / "examples" / "persona-card").glob("*.json"))
        self.assertGreater(len(cases), 5)
        for f in cases:
            with self.subTest(case=f.name):
                c = json.loads(f.read_text())
                self.assertEqual(self.v.validate("PersonaCard", c["card"]), [])
                if c["view"] is not None:
                    self.assertEqual(self.v.validate("View", c["view"]), [])

    def test_showcase_snapshots_hold_to_the_schema(self):
        # examples/showcase/<page>.json: every page of the 组件展示 extension (persona-extensions, #1852), which each
        # app's screenshot tests draw; copied from the extension's snapshots/.
        pages = sorted((SCHEMA_DIR / "examples" / "showcase").glob("*.json"))
        self.assertGreater(len(pages), 30)
        seen = set()
        for f in pages:
            with self.subTest(page=f.name):
                v = json.loads(f.read_text())
                self.assertEqual(self.v.validate("View", v), [])
                seen |= {i["type"] for s in v["sections"] for i in s["items"]}
        view = self.v.files["view.schema.json"]["$defs"]
        components = [view[o["$ref"].split("/")[-1]] for o in view["Component"]["oneOf"]]
        tags = {c["properties"]["type"]["const"] for c in components if not c.get("x-assistant-only")}
        self.assertEqual(tags - seen, set(), "the showcase shows every extension component")

    def test_every_component_has_an_example(self):
        view = self.v.files["view.schema.json"]["$defs"]
        tags = {view[o["$ref"].split("/")[-1]]["properties"]["type"]["const"] for o in view["Component"]["oneOf"]}
        seen = set()
        for f in (SCHEMA_DIR / "examples").glob("View.*.json"):
            for s in json.loads(f.read_text())["sections"]:
                seen |= {i["type"] for i in s["items"]}
        for f in (SCHEMA_DIR / "examples" / "assistant").glob("*.json"):
            v = json.loads(f.read_text())
            self.assertEqual(self.v.validate("View", v), [])
            for s in v["sections"]:
                seen |= {i["type"] for i in s["items"]}
        self.assertEqual(tags - seen, set())


if __name__ == "__main__":
    unittest.main()
