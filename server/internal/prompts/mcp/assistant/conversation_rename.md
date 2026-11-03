Rename a Persona conversation. Titles read 「MMDD | 类型 | 主题」: give `kind` and `topic` and the hub puts the day the conversation started in front. A title the user gave stays: renaming one fails unless the user asked for this rename in so many words (then pass `user_asked`). The main conversation has no title to change.

```json
{
  "type": "object",
  "required": ["id", "kind", "topic"],
  "properties": {
    "id": {
      "type": "string",
      "description": "full id or its first 8 characters; this conversation's own id renames it"
    },
    "kind": {
      "type": "string",
      "enum": ["功能", "设计", "修复", "优化", "发布", "探索", "文档", "研究", "评审", "定时"]
    },
    "topic": {
      "type": "string",
      "description": "what it is about, at most 14 characters"
    },
    "user_asked": {
      "type": "boolean",
      "description": "the user asked for this rename; needed to replace a title they gave"
    }
  }
}
```
