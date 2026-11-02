-- When a job first hit a rate limit in its current streak; cleared when it makes progress.
ALTER TABLE uploads ADD COLUMN rate_limited_since INTEGER;
