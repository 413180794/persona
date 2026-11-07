"""Pinned pre-cutover protocol identities, not product copy.

These fixtures correspond to the request/response contracts in 39a706a338.
Only this file carries retired spellings for the repeatable mixed-version suite.
"""
LEGACY_CLIENT_HEADER = "X-Jarvis-Client"
LEGACY_OP_HEADER = "X-Jarvis-Op-Id"
LEGACY_START_KEY = "started_by_jarvis"
LEGACY_ROOT = ".jarvis"
LEGACY_CONTEXT = '<jarvis-context kind="context" title="Fixture">Fixture context</jarvis-context>\n\n'

def paired_env(values):
    out = dict(values)
    for key, value in values.items():
        if key.startswith("PERSONA_"):
            out["JARVIS_" + key.removeprefix("PERSONA_")] = value
    return out

def request_headers(token, op_id=None, canonical=False):
    out = {"Authorization": "Bearer " + token, "Content-Type": "application/json"}
    out["X-Persona-Client" if canonical else LEGACY_CLIENT_HEADER] = "web/compat-fixture"
    if op_id:
        out["X-Persona-Op-Id" if canonical else LEGACY_OP_HEADER] = op_id
    return out
