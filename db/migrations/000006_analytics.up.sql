-- 000006 analytics.
-- Raw view events are partitioned by month so retention = DROP PARTITION (instant, no bloat, no vacuum).
-- Dashboards read the small daily rollups, never the raw events. See docs/DESIGN.md for the scaling path.

CREATE SEQUENCE video_views_id_seq;

CREATE TABLE video_views (
  id                     bigint NOT NULL DEFAULT nextval('video_views_id_seq'),
  video_id               uuid NOT NULL,       -- no FK on purpose: high-volume event table, integrity enforced by the writer
  user_id                uuid,                -- no FK on purpose: account deletion must not rewrite billions of rows
  session_id             uuid,                -- random viewer/session key, not an FK
  watch_duration_seconds integer NOT NULL DEFAULT 0,
  completion_percentage  numeric(5, 2) NOT NULL DEFAULT 0,
  ip_hash                bytea,               -- HMAC(daily-rotating salt, ip): cannot be reversed or tracked across days
  country_code           char(2),
  device_type            device_type NOT NULL DEFAULT 'OTHER',
  traffic_source         traffic_source NOT NULL DEFAULT 'OTHER',
  created_at             timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id, created_at),               -- the partition key must be part of the primary key
  CONSTRAINT video_views_duration CHECK (watch_duration_seconds >= 0),
  CONSTRAINT video_views_completion CHECK (completion_percentage BETWEEN 0 AND 100),
  CONSTRAINT video_views_ip_hash_len CHECK (ip_hash IS NULL OR octet_length(ip_hash) = 32),
  CONSTRAINT video_views_country CHECK (country_code IS NULL OR country_code ~ '^[A-Z]{2}$')
) PARTITION BY RANGE (created_at);
ALTER SEQUENCE video_views_id_seq OWNED BY video_views.id;
-- Per-video recent window (abuse checks, per-video drill-down). Created on every partition automatically.
CREATE INDEX video_views_by_video ON video_views (video_id, created_at);

CREATE FUNCTION ensure_monthly_partitions(p_parent regclass, p_months_ahead integer DEFAULT 3)
RETURNS integer LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $$
DECLARE
  parent_name text;
  month_start date;
  part_name   text;
  created     integer := 0;
BEGIN
  SELECT relname INTO parent_name FROM pg_class WHERE oid = p_parent;
  FOR i IN 0..p_months_ahead LOOP
    month_start := (date_trunc('month', now() AT TIME ZONE 'UTC') + make_interval(months => i))::date;
    part_name := format('%s_y%sm%s', parent_name, to_char(month_start, 'YYYY'), to_char(month_start, 'MM'));
    IF to_regclass(quote_ident(part_name)) IS NULL THEN
      EXECUTE format(
        'CREATE TABLE %I PARTITION OF %s FOR VALUES FROM (%L) TO (%L)',
        part_name, p_parent,
        to_char(month_start, 'YYYY-MM-DD') || ' 00:00:00+00',
        to_char((month_start + interval '1 month')::date, 'YYYY-MM-DD') || ' 00:00:00+00'
      );
      created := created + 1;
    END IF;
  END LOOP;
  RETURN created;
END $$;

-- Drops monthly partitions whose whole month is older than p_keep_months. Names must follow <parent>_yYYYYmMM.
CREATE FUNCTION drop_old_partitions(p_parent regclass, p_keep_months integer)
RETURNS integer LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $$
DECLARE
  cutoff  date := (date_trunc('month', now() AT TIME ZONE 'UTC') - make_interval(months => p_keep_months))::date;
  child   record;
  m       text[];
  dropped integer := 0;
BEGIN
  FOR child IN
    SELECT c.relname
    FROM pg_inherits i JOIN pg_class c ON c.oid = i.inhrelid
    WHERE i.inhparent = p_parent
  LOOP
    m := regexp_match(child.relname, '_y([0-9]{4})m([0-9]{2})$');
    IF m IS NOT NULL AND make_date(m[1]::int, m[2]::int, 1) < cutoff THEN
      EXECUTE format('DROP TABLE %I', child.relname);
      dropped := dropped + 1;
    END IF;
  END LOOP;
  RETURN dropped;
END $$;

SELECT ensure_monthly_partitions('video_views', 3);   -- current month + 3 ahead; a monthly job keeps this rolling

-- Rollups. Counters are ADDED to by batch upserts (see db/queries/examples.sql), so replays must be idempotent
-- at the job level (track the last processed window), not at the row level.
CREATE TABLE video_daily_analytics (
  video_id           uuid NOT NULL REFERENCES videos (id) ON DELETE CASCADE,
  date               date NOT NULL,                              -- UTC day
  views              integer NOT NULL DEFAULT 0,
  watch_time_seconds bigint NOT NULL DEFAULT 0,
  likes              integer NOT NULL DEFAULT 0,                 -- new likes that day
  comments           integer NOT NULL DEFAULT 0,                 -- new comments that day
  unique_viewers     integer NOT NULL DEFAULT 0,                 -- from Redis HyperLogLog at day close (not additive)
  PRIMARY KEY (video_id, date),
  CONSTRAINT video_daily_non_negative CHECK (
    views >= 0 AND watch_time_seconds >= 0 AND likes >= 0 AND comments >= 0 AND unique_viewers >= 0
  )
);
CREATE INDEX video_daily_trending ON video_daily_analytics (date, views DESC);   -- "trending in the last N days"

CREATE TABLE creator_daily_analytics (
  creator_id         uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  date               date NOT NULL,
  views              bigint NOT NULL DEFAULT 0,
  watch_time_seconds bigint NOT NULL DEFAULT 0,
  likes              integer NOT NULL DEFAULT 0,
  comments           integer NOT NULL DEFAULT 0,
  new_subscribers    integer NOT NULL DEFAULT 0,
  video_uploads      integer NOT NULL DEFAULT 0,
  PRIMARY KEY (creator_id, date),
  CONSTRAINT creator_daily_non_negative CHECK (
    views >= 0 AND watch_time_seconds >= 0 AND likes >= 0 AND comments >= 0 AND new_subscribers >= 0 AND video_uploads >= 0
  )
);

-- Studio "Audience" and "Traffic sources" panels (top countries / devices / sources per day).
CREATE TABLE creator_daily_breakdown (
  creator_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  date       date NOT NULL,
  dimension  breakdown_dimension NOT NULL,
  key        text NOT NULL,                                      -- 'US', 'MOBILE', 'SEARCH', ...
  views      bigint NOT NULL DEFAULT 0,
  PRIMARY KEY (creator_id, date, dimension, key),
  CONSTRAINT creator_breakdown_views CHECK (views >= 0),
  CONSTRAINT creator_breakdown_key_len CHECK (char_length(key) BETWEEN 1 AND 32)
);
