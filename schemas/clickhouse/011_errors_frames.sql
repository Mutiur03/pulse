-- Symbolicated frames (JSON) next to the raw stack, and the debug ids the
-- SDK reported for the generated files.
ALTER TABLE errors ADD COLUMN IF NOT EXISTS frames String DEFAULT '' AFTER culprit;
ALTER TABLE errors ADD COLUMN IF NOT EXISTS debug_ids String DEFAULT '' AFTER frames;
ALTER TABLE errors ADD COLUMN IF NOT EXISTS mapped_frames UInt16 DEFAULT 0 AFTER debug_ids;
