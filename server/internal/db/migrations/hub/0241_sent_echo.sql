-- What the node knows of where a sent message landed in the transcript (node/echo.go): the record it expects the copy
-- by (start of the text, the transcript's last line before it was typed, when), and once found the line (its items'
-- id before "#"). The hub hands these back to a node that started again (session.echoes), so items keep their
-- client_msg_id and a message still expected is found after the restart.
ALTER TABLE sent_messages ADD COLUMN IF NOT EXISTS echo jsonb;
