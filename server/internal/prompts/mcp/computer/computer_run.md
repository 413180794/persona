Act on one of the user's Macs through Cua Driver, in one call: either `steps` (Cua's run_steps: actions in order, stopping at the first failure) or `script` (Cua's run_script: JavaScript with loops and checks). Apps act in the background without taking focus.
- steps: `[{"click": {"app": "Calculator", "role": "button", "name": "7"}}, {"press_key": {"app": "Calculator", "key": "return"}}]`. A step names its target by role/name (no earlier read needed) or by an `element_token` from computer_state. Other actions: double_click, right_click, type_text {text}, set_value {value}, hotkey {keys}, scroll, launch_app {name}. Add `observe: {}` to get what changed in the window after the last step (no screenshot).
- script: the body of an async function with one API, `cua`: `const app = await cua.getApp("Calculator"); await app.click({role: "button", name: "7"}); const [d] = await app.query({role: "statictext"}); return d.value;`. Also `cua.launch(name)`, `app.typeText(text, target?)`, `app.pressKey("cmd+s")`, `app.waitFor({…})`, `app.getState()`. Its return value comes back; `timeout_ms` up to 120000.
Read first with computer_state when unsure what is on screen, and read again with computer_state for a screenshot to show the user the result. Writes may be confirmed on the phone first, if the user set that.

```json
{
  "type": "object",
  "required": ["machine"],
  "properties": {
    "machine": {
      "type": "string",
      "description": "machine id or name"
    },
    "steps": {
      "type": "array",
      "items": {
        "type": "object"
      },
      "description": "Cua run_steps steps"
    },
    "app": {
      "type": "string",
      "description": "default app for steps that name none"
    },
    "observe": {
      "type": "object",
      "description": "after the steps, read what changed in the window (get_window_state options; {} for defaults)"
    },
    "script": {
      "type": "string",
      "description": "Cua run_script JavaScript"
    },
    "timeout_ms": {
      "type": "integer",
      "description": "script wall time, default 30000, max 120000"
    }
  }
}
```
