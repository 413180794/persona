Hand the user a plan of one or more tasks for agents on their computers, instead of starting agents yourself. Use it for any work that takes an agent (code, research in a repo, anything longer than a few tool calls); one task per independent piece of work. Fill in every field from what you know (the knowledge base, agents_list: which machine works in which directory, the user's habits); the user sees each choice as a chip they can change, with your one-line reason. Read-only tasks, tasks the user asked for themselves (user_requested), and kinds the user told you to start without asking, start at once; the rest wait as a card for the user's 开始. Either way this returns at once: don't wait, carry on. Each started task is a todo (todo id) whose progress shows in your context snapshot; send to a task's agent with agent_send. Quick things you can do with a tool yourself (a reminder, an event) are not tasks. A task whose launcher failed its last health check on its machine moves to a machine where it works; its why says so.

```json
{
  "type": "object",
  "required": ["tasks"],
  "properties": {
    "tasks": {
      "type": "array",
      "minItems": 1,
      "maxItems": 8,
      "items": {
        "type": "object",
        "required": ["title", "prompt", "machine", "cwd", "readonly", "user_requested", "why"],
        "properties": {
          "title": {
            "type": "string",
            "description": "the task in a short line the user reads on the card (Chinese, under 20 characters)"
          },
          "prompt": {
            "type": "string",
            "description": "the agent's first message: everything it needs to do the task without asking you"
          },
          "machine": {
            "type": "string",
            "description": "machine id"
          },
          "cwd": {
            "type": "string",
            "description": "the project directory: absolute path or ~/…"
          },
          "launcher": {
            "type": "string",
            "description": "launcher name; empty: the default"
          },
          "model": {
            "type": "string",
            "description": "model; empty: the launcher's default"
          },
          "permission": {
            "type": "string",
            "enum": ["default", "acceptEdits", "plan", "bypassPermissions"],
            "description": "permission mode; empty: the default"
          },
          "readonly": {
            "type": "boolean",
            "description": "true when the task only reads (looks things up, reads code, writes nothing anywhere)"
          },
          "user_requested": {
            "type": "boolean",
            "description": "true when the user asked for this very task in their latest message (told you to do it, to have an agent do it, to go ahead and start); false when it is your own idea or inferred from something they did not ask for"
          },
          "why": {
            "type": "string",
            "description": "one line (Chinese) on why this machine, directory and model"
          }
        }
      }
    }
  }
}
```
