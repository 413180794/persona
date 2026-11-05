See one of the user's Macs through Cua Driver: with `app` (a name like "Calculator" or a bundle id), that app's frontmost window as an accessibility tree plus a screenshot; without it, the apps and windows on screen plus a screenshot of the whole display. Rows of a window's tree are `[N]`; act on them in computer_run with `element_token` "<snapshot_id>:N" (the snapshot_id is in the result; tokens stay valid in this conversation until the window is read again). A long tree is cut: pass `query` to keep only matching rows. Only machines the user turned this on for, and only while someone's desktop is unlocked on them. On a machine whose backend is LCU this is one `js` call (`cua.getState()`, or `cua.getApp` then that app's state). If the runtime asks to allow an app, the user answers on the phone or the web; do not assume it was allowed.

```json
{
  "type": "object",
  "required": ["machine"],
  "properties": {
    "machine": {
      "type": "string",
      "description": "machine id or name"
    },
    "app": {
      "type": "string",
      "description": "app name or bundle id; omit for the whole screen"
    },
    "query": {
      "type": "string",
      "description": "keep only tree rows containing this (case-insensitive)"
    },
    "screenshot": {
      "type": "boolean",
      "description": "default true; false returns the tree only"
    }
  }
}
```
