-- When the job's current streak of rate limits began; cleared when an attempt ends for another reason.
ALTER TABLE uploads ADD COLUMN rate_limited_since INTEGER;
