# Feishu report presentation

Read this guidance when a user requests a report, callout/highlight improvements, diagrams or HTML-to-PNG illustrations. MCP clients read it as the resource `lark-connector://guides/document-presentation` (`resources/read`, `text/markdown`); the former `get_document_presentation_guide` tool is no longer listed in `tools/list`, so the static text costs no tokens there, but direct calls by that name still return the guide with a pointer to the resource. It is bundled local guidance: no account, backend request, CLI execution or document write occurs, and it remains available in read-only mode.

Read the canonical [document-presentation.md](../../../../plugins/lark-connector/skills/lark-connector/references/document-presentation.md) for native callout examples, diagram selection, Mermaid/PNG tradeoffs, insertion placement and readback/render checks. The same file is embedded in the MCP bundle and served by that resource; do not maintain a second copy of its formatting rules here.

Actual document edits require an available writer and the user's authorization. Use `docs-editing.md` for exposed Agent routes, or the selected writer's current skills and command help for its operations. Do not send DocxXML as a chat reply, invent missing data for a chart, or claim a document was styled merely because the guide was retrieved.
