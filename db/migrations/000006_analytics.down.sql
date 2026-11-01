DROP TABLE IF EXISTS creator_daily_breakdown;
DROP TABLE IF EXISTS creator_daily_analytics;
DROP TABLE IF EXISTS video_daily_analytics;
DROP TABLE IF EXISTS video_views;           -- drops all monthly partitions with it
DROP FUNCTION IF EXISTS drop_old_partitions(regclass, integer);
DROP FUNCTION IF EXISTS ensure_monthly_partitions(regclass, integer);
DROP SEQUENCE IF EXISTS video_views_id_seq;
