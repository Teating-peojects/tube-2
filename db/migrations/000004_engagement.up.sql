-- 000004 engagement: history, reactions, comments, subscriptions, playlists, notifications, etc.
-- Convention: every FK child column that can be hit by a cascade/restrict has a supporting index.

CREATE TABLE watch_history (
  user_id               uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  video_id              uuid NOT NULL REFERENCES videos (id) ON DELETE CASCADE,
  last_position_seconds integer NOT NULL DEFAULT 0,
  watch_duration_seconds integer NOT NULL DEFAULT 0,
  completed             boolean NOT NULL DEFAULT false,
  last_watched_at       timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, video_id),
  CONSTRAINT watch_history_non_negative CHECK (last_position_seconds >= 0 AND watch_duration_seconds >= 0)
);
-- GET /history (keyset pagination, newest first)
CREATE INDEX watch_history_recent ON watch_history (user_id, last_watched_at DESC, video_id DESC);
-- "Continue watching" strip on Home
CREATE INDEX watch_history_continue ON watch_history (user_id, last_watched_at DESC)
  WHERE completed = false AND last_position_seconds > 0;
CREATE INDEX watch_history_by_video ON watch_history (video_id);   -- FK cascade

CREATE TABLE video_reactions (
  user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  video_id   uuid NOT NULL REFERENCES videos (id) ON DELETE CASCADE,
  reaction   reaction_type NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, video_id)                              -- one reaction per user per video
);
CREATE INDEX video_reactions_by_video ON video_reactions (video_id, reaction);        -- recounts + FK cascade
CREATE INDEX video_reactions_liked ON video_reactions (user_id, created_at DESC, video_id) WHERE reaction = 'LIKE';  -- GET /liked

CREATE TABLE comments (
  id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  video_id          uuid NOT NULL,
  video_creator_id  uuid NOT NULL,                             -- denormalised on purpose: Studio inbox query (kept consistent by FK below)
  user_id           uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  parent_comment_id uuid,
  content           text NOT NULL,
  status            comment_status NOT NULL DEFAULT 'VISIBLE',
  is_pinned         boolean NOT NULL DEFAULT false,
  like_count        integer NOT NULL DEFAULT 0,                -- trigger-maintained
  reply_count       integer NOT NULL DEFAULT 0,                -- trigger-maintained (visible replies)
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  deleted_at        timestamptz,
  CONSTRAINT comments_video_fk FOREIGN KEY (video_id, video_creator_id)
    REFERENCES videos (id, creator_id) ON DELETE CASCADE,
  CONSTRAINT comments_id_video_key UNIQUE (id, video_id),
  -- Parent must be a comment on the SAME video (no trigger needed).
  CONSTRAINT comments_parent_fk FOREIGN KEY (parent_comment_id, video_id)
    REFERENCES comments (id, video_id) ON DELETE CASCADE,
  CONSTRAINT comments_content_len CHECK (char_length(btrim(content)) BETWEEN 1 AND 2000),
  CONSTRAINT comments_pin_top_level CHECK (NOT is_pinned OR parent_comment_id IS NULL),
  CONSTRAINT comments_counters CHECK (like_count >= 0 AND reply_count >= 0),
  CONSTRAINT comments_deleted_consistency CHECK ((status = 'DELETED') = (deleted_at IS NOT NULL))
);
-- Top-level comments, newest first / most liked first (keyset pagination). Replies use comments_replies.
CREATE INDEX comments_video_new ON comments (video_id, created_at DESC, id DESC)
  WHERE parent_comment_id IS NULL AND status = 'VISIBLE';
CREATE INDEX comments_video_top ON comments (video_id, like_count DESC, created_at DESC, id DESC)
  WHERE parent_comment_id IS NULL AND status = 'VISIBLE';
CREATE UNIQUE INDEX comments_one_pinned ON comments (video_id) WHERE is_pinned;      -- at most one pinned comment per video
CREATE INDEX comments_replies ON comments (parent_comment_id, created_at, id) WHERE parent_comment_id IS NOT NULL;  -- + FK cascade
CREATE INDEX comments_by_user ON comments (user_id, created_at DESC);                -- profile / FK cascade
CREATE INDEX comments_by_video ON comments (video_id);                               -- FK cascade from videos
-- Studio > Comments: everything on my videos, and the "held for review" queue.
CREATE INDEX comments_creator_inbox ON comments (video_creator_id, created_at DESC, id DESC)
  WHERE status IN ('VISIBLE', 'UNDER_REVIEW');
CREATE INDEX comments_creator_review ON comments (video_creator_id, created_at DESC, id DESC)
  WHERE status = 'UNDER_REVIEW';

CREATE TABLE comment_reactions (
  user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  comment_id uuid NOT NULL REFERENCES comments (id) ON DELETE CASCADE,
  reaction   reaction_type NOT NULL DEFAULT 'LIKE',
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, comment_id)
);
CREATE INDEX comment_reactions_by_comment ON comment_reactions (comment_id);

-- "subscriptions" = following a creator (free). Paid plans are a separate, deferred design.
CREATE TABLE subscriptions (
  subscriber_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  creator_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  created_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (subscriber_id, creator_id),
  CONSTRAINT subscriptions_no_self CHECK (subscriber_id <> creator_id)
);
CREATE INDEX subscriptions_by_creator ON subscriptions (creator_id, created_at DESC);
CREATE INDEX subscriptions_by_subscriber ON subscriptions (subscriber_id, created_at DESC);

CREATE TABLE playlists (
  id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id            uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  title              text NOT NULL,
  description        text NOT NULL DEFAULT '',
  visibility         playlist_visibility NOT NULL DEFAULT 'PRIVATE',
  thumbnail_video_id uuid REFERENCES videos (id) ON DELETE SET NULL,   -- optional override; default = first video
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  deleted_at         timestamptz,
  CONSTRAINT playlists_title_len CHECK (char_length(btrim(title)) BETWEEN 1 AND 100),
  CONSTRAINT playlists_description_len CHECK (char_length(description) <= 1000)
);
CREATE INDEX playlists_by_user ON playlists (user_id, updated_at DESC, id DESC) WHERE deleted_at IS NULL;
CREATE INDEX playlists_public_by_user ON playlists (user_id, updated_at DESC, id DESC) WHERE visibility = 'PUBLIC' AND deleted_at IS NULL;
CREATE INDEX playlists_public_title_trgm ON playlists USING gin (title gin_trgm_ops) WHERE visibility = 'PUBLIC' AND deleted_at IS NULL;
CREATE INDEX playlists_thumbnail_video ON playlists (thumbnail_video_id) WHERE thumbnail_video_id IS NOT NULL;

CREATE TABLE playlist_videos (
  playlist_id uuid NOT NULL REFERENCES playlists (id) ON DELETE CASCADE,
  video_id    uuid NOT NULL REFERENCES videos (id) ON DELETE CASCADE,
  position    integer NOT NULL,
  added_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (playlist_id, video_id),
  CONSTRAINT playlist_videos_position_non_negative CHECK (position >= 0),
  -- Deferred so a reorder can swap positions inside one transaction/statement.
  CONSTRAINT playlist_videos_position_key UNIQUE (playlist_id, position) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX playlist_videos_by_video ON playlist_videos (video_id);   -- "which of my playlists contain this video" + FK

CREATE TABLE watch_later (
  user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  video_id   uuid NOT NULL REFERENCES videos (id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, video_id)
);
CREATE INDEX watch_later_recent ON watch_later (user_id, created_at DESC, video_id DESC);
CREATE INDEX watch_later_by_video ON watch_later (video_id);

CREATE TABLE blocked_users (
  user_id         uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  blocked_user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  created_at      timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, blocked_user_id),
  CONSTRAINT blocked_users_no_self CHECK (user_id <> blocked_user_id)
);
CREATE INDEX blocked_users_by_blocked ON blocked_users (blocked_user_id);

-- Frontend "Not interested" action (feed personalisation).
CREATE TABLE not_interested (
  user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  video_id   uuid NOT NULL REFERENCES videos (id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, video_id)
);
CREATE INDEX not_interested_by_video ON not_interested (video_id);

-- NOTE: do not fan out NEW_VIDEO rows for creators with huge audiences; compute that feed on read.
CREATE TABLE notifications (
  id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  user_id       uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  type          notification_type NOT NULL,
  actor_user_id uuid REFERENCES users (id) ON DELETE SET NULL,
  video_id      uuid REFERENCES videos (id) ON DELETE CASCADE,
  comment_id    uuid REFERENCES comments (id) ON DELETE CASCADE,
  payload       jsonb NOT NULL DEFAULT '{}'::jsonb,            -- small, type-specific extras; title/body are built at read time
  read_at       timestamptz,
  is_read       boolean GENERATED ALWAYS AS (read_at IS NOT NULL) STORED,   -- derived: can never disagree with read_at
  created_at    timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT notifications_payload CHECK (jsonb_typeof(payload) = 'object' AND octet_length(payload::text) <= 4096)
);
CREATE INDEX notifications_unread ON notifications (user_id, created_at DESC, id DESC) WHERE read_at IS NULL;
CREATE INDEX notifications_by_user ON notifications (user_id, created_at DESC, id DESC);
CREATE INDEX notifications_by_video ON notifications (video_id) WHERE video_id IS NOT NULL;      -- FK cascade
CREATE INDEX notifications_by_comment ON notifications (comment_id) WHERE comment_id IS NOT NULL; -- FK cascade
CREATE INDEX notifications_by_actor ON notifications (actor_user_id) WHERE actor_user_id IS NOT NULL;  -- FK set null

CREATE TABLE search_history (
  id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  query      citext NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT search_history_user_query_key UNIQUE (user_id, query),   -- repeat searches just refresh created_at
  CONSTRAINT search_history_query_len CHECK (char_length(btrim(query)) BETWEEN 1 AND 100)
);
CREATE INDEX search_history_recent ON search_history (user_id, created_at DESC);
CREATE INDEX search_history_created_at ON search_history (created_at);   -- retention job
