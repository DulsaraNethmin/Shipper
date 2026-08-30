-- Reverse of 000805.
--
-- The indexes go with the table; naming them would be noise. There is no trigger to drop — reports
-- are appended and never edited, so the table carries no updated_at and no set_updated_at().

DROP TABLE IF EXISTS reports;
