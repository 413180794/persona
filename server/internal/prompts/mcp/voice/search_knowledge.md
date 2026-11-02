搜索用户的知识库（聊天记录归档、人、群、项目、文档、会议的摘要）。返回匹配的行，按时间从新到旧。关键词用空格分开，每个词都要命中；用人名、项目名、群名这类具体的词。

```json
{
  "type": "object",
  "required": ["query"],
  "properties": {
    "query": {"type": "string", "description": "关键词，空格分隔"}
  }
}
```
