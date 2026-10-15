-- Full span attributes for the trace detail panel, and non-exception span
-- events (JSON array of {name, time, attributes}).
ALTER TABLE spans ADD COLUMN IF NOT EXISTS attributes Map(String, String) AFTER session_id;
ALTER TABLE spans ADD COLUMN IF NOT EXISTS span_events String DEFAULT '[]' AFTER attributes;
