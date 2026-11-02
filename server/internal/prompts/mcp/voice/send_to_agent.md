给某个 Agent 发一条消息（相当于用户在它的输入框里打字并回车）。只在用户明确要求时用；发之前先把要发给谁、发什么复述一遍，等用户口头确认。machine_id 和 session_id 来自 list_agents。

```json
{
  "type": "object",
  "required": ["machine_id", "session_id", "text"],
  "properties": {
    "machine_id": {"type": "string"},
    "session_id": {"type": "string"},
    "text": {"type": "string", "description": "要发的内容"}
  }
}
```
