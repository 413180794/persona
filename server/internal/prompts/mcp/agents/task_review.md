Review a task you handed out from this conversation (its todo id), after you checked its work yourself: each done_when criterion with its evidence (a PR link and that it merged, CI, a screenshot, what the live system answers) or what is missing. accepted closes the task (its agent is closed once it stays still); rework sends what is missing back to the same agent (resumed if it was closed) — at most 2 rounds, a third goes to the user instead; needs_user marks it waiting for the user's decision (ask them yourself: ask_user, or notify_user if they are away); cancelled ends it (a duplicate, no longer wanted). From a conversation other than the main one, on a task it does not supervise, only accepted and cancelled apply: send what is missing to its agent with agent_send. Use it on every task a 督办 message says waits for review; don't accept on the agent's word alone.

```json
{
  "type": "object",
  "required": ["todo_id", "verdict"],
  "properties": {
    "todo_id": {
      "type": "string",
      "description": "the task's todo id (todo_…), from the 督办 message or the context snapshot"
    },
    "verdict": {
      "type": "string",
      "enum": ["accepted", "rework", "needs_user", "cancelled"]
    },
    "checks": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["criterion", "met"],
        "properties": {
          "criterion": {"type": "string", "description": "one done_when criterion (or what the task was for, when it had none)"},
          "met": {"type": "boolean"},
          "evidence": {"type": "string", "description": "what shows it is met (a link, a command's output), or what is missing"}
        }
      },
      "description": "each criterion, met or not, with its evidence or gap"
    },
    "note": {
      "type": "string",
      "description": "rework: what the agent is to do (Chinese, concrete); needs_user: what the user is to decide; cancelled: why; accepted: the result in a line"
    }
  }
}
```
