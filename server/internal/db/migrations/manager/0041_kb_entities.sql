-- Index of the knowledge base under ~/.persona/data (hub/kb.go, docs/assistant/knowledge.md): the files are the source of
-- truth, this table only ranks and filters them, and can be rebuilt from the files.
CREATE TABLE IF NOT EXISTS kb_entities (
    kind       text NOT NULL,          -- chats | people | projects | docs | meetings | events
    id         text NOT NULL,
    name       text NOT NULL,
    importance text NOT NULL DEFAULT '',
    relevance  int  NOT NULL DEFAULT 0,
    tags       text[] NOT NULL DEFAULT '{}',
    one_line   text NOT NULL DEFAULT '',
    updated_at timestamptz,
    PRIMARY KEY (kind, id)
);
CREATE INDEX IF NOT EXISTS kb_entities_rank ON kb_entities (kind, importance, relevance DESC);
CREATE INDEX IF NOT EXISTS kb_entities_tags ON kb_entities USING gin (tags);
