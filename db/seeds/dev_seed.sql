-- DEVELOPMENT SEED ONLY. Safe to re-run (ON CONFLICT DO NOTHING). Never run in production.
-- No real passwords: every account gets a syntactically valid but UNUSABLE placeholder hash.
-- Set a dev password with:  DATABASE_URL=... DEV_PASSWORD=... go run ./cmd/devpass admin@redxtube.test   (local databases only)
-- (the frontend's mock "demo / Demo-12345" account exists only inside the mock API, not here).
-- Triggers create profiles, settings and video_stats automatically.

BEGIN;

INSERT INTO users (id, username, email, password_hash, display_name, role, status, is_verified, is_creator) VALUES
  ('00000000-0000-7000-8000-000000000001', 'admin',     'admin@redxtube.test',   '$argon2id$v=19$m=65536,t=3,p=4$UNUSABLE$UNUSABLE', 'Site Admin',     'SUPER_ADMIN', 'ACTIVE', true, false),
  ('00000000-0000-7000-8000-000000000002', 'moderator', 'mod@redxtube.test',     '$argon2id$v=19$m=65536,t=3,p=4$UNUSABLE$UNUSABLE', 'Moderator One',  'MODERATOR',   'ACTIVE', true, false),
  ('00000000-0000-7000-8000-000000000010', 'creator1',  'creator1@redxtube.test','$argon2id$v=19$m=65536,t=3,p=4$UNUSABLE$UNUSABLE', 'Creator Name',   'CREATOR',     'ACTIVE', true, true),
  ('00000000-0000-7000-8000-000000000011', 'studio_x',  'creator2@redxtube.test','$argon2id$v=19$m=65536,t=3,p=4$UNUSABLE$UNUSABLE', 'Studio Name',    'CREATOR',     'ACTIVE', true, true),
  ('00000000-0000-7000-8000-000000000020', 'viewer1',   'viewer1@redxtube.test', '$argon2id$v=19$m=65536,t=3,p=4$UNUSABLE$UNUSABLE', 'Test Viewer',    'USER',        'ACTIVE', true, false),
  ('00000000-0000-7000-8000-000000000021', 'viewer2',   'viewer2@redxtube.test', '$argon2id$v=19$m=65536,t=3,p=4$UNUSABLE$UNUSABLE', 'Second Viewer',  'USER',        'PENDING', false, false)
ON CONFLICT DO NOTHING;

INSERT INTO categories (name, slug, description, sort_order) VALUES
  ('Entertainment', 'entertainment', 'General entertainment', 10),
  ('Music',         'music',         'Music and performances', 20),
  ('Gaming',        'gaming',        'Gaming content',         30),
  ('Shorts',        'shorts',        'Short-form videos',      40),
  ('Documentary',   'documentary',   'Documentary films',      50),
  ('Comedy',        'comedy',        'Comedy',                 60),
  ('Education',     'education',     'Educational content',    70),
  ('Other',         'other',         'Everything else',        99)
ON CONFLICT DO NOTHING;

INSERT INTO tags (name, slug) VALUES
  ('cinematic', 'cinematic'), ('neon', 'neon'), ('city', 'city'), ('studio', 'studio'),
  ('interview', 'interview'), ('montage', 'montage')
ON CONFLICT DO NOTHING;

INSERT INTO age_verifications (user_id, status, method, verified_at, expires_at) VALUES
  ('00000000-0000-7000-8000-000000000020', 'VERIFIED', 'SELF_DECLARATION', now(), now() + interval '30 days')
ON CONFLICT DO NOTHING;

-- Sample videos (metadata only; storage keys point at non-existent dev objects).
INSERT INTO videos (id, creator_id, title, slug, description, video_type, visibility, status, duration_seconds,
                    file_size_bytes, original_filename, original_storage_key, hls_master_key, category_id, language,
                    published_at) VALUES
  ('00000000-0000-7000-8000-000000000101', '00000000-0000-7000-8000-000000000010', 'Neon City Nights: A Cinematic Drive', 'neon-city-nights',
   'Sample description for the first video.', 'LONG', 'PUBLIC', 'PUBLISHED', 1122, 734003200, 'neon.mp4',
   'orig/00000000-0000-7000-8000-000000000101/neon.mp4', 'hls/00000000-0000-7000-8000-000000000101/master.m3u8',
   (SELECT id FROM categories WHERE slug = 'entertainment'), 'en', now() - interval '3 days'),
  ('00000000-0000-7000-8000-000000000102', '00000000-0000-7000-8000-000000000010', 'Behind the Scenes: Studio Session', 'studio-session',
   'Sample description.', 'LONG', 'PUBLIC', 'PUBLISHED', 1445, 912261120, 'bts.mp4',
   'orig/00000000-0000-7000-8000-000000000102/bts.mp4', 'hls/00000000-0000-7000-8000-000000000102/master.m3u8',
   (SELECT id FROM categories WHERE slug = 'entertainment'), 'en', now() - interval '5 days'),
  ('00000000-0000-7000-8000-000000000103', '00000000-0000-7000-8000-000000000011', 'Short Film: The Last Train', 'last-train',
   'Sample description.', 'LONG', 'UNLISTED', 'PUBLISHED', 408, 220200960, 'train.mp4',
   'orig/00000000-0000-7000-8000-000000000103/train.mp4', 'hls/00000000-0000-7000-8000-000000000103/master.m3u8',
   (SELECT id FROM categories WHERE slug = 'documentary'), 'en', now() - interval '8 days'),
  ('00000000-0000-7000-8000-000000000104', '00000000-0000-7000-8000-000000000011', 'Quick Clip', 'quick-clip',
   'A short clip.', 'SHORT', 'PUBLIC', 'PUBLISHED', 42, 31457280, 'clip.mp4',
   'orig/00000000-0000-7000-8000-000000000104/clip.mp4', 'hls/00000000-0000-7000-8000-000000000104/master.m3u8',
   (SELECT id FROM categories WHERE slug = 'shorts'), 'en', now() - interval '1 day')
ON CONFLICT DO NOTHING;

-- A draft that is still processing (shows up in Studio > Processing).
INSERT INTO videos (id, creator_id, title, visibility, status, file_size_bytes, original_filename, original_storage_key)
VALUES ('00000000-0000-7000-8000-000000000105', '00000000-0000-7000-8000-000000000010', 'Work in progress', 'PRIVATE', 'PROCESSING',
        104857600, 'wip.mp4', 'orig/00000000-0000-7000-8000-000000000105/wip.mp4')
ON CONFLICT DO NOTHING;

INSERT INTO video_metadata (video_id, width, height, fps, video_codec, audio_codec, bitrate, container) VALUES
  ('00000000-0000-7000-8000-000000000101', 1920, 1080, 30, 'h264', 'aac', 6000000, 'mp4'),
  ('00000000-0000-7000-8000-000000000102', 1920, 1080, 30, 'h264', 'aac', 6000000, 'mp4'),
  ('00000000-0000-7000-8000-000000000103', 1280, 720, 24, 'h264', 'aac', 3500000, 'mp4'),
  ('00000000-0000-7000-8000-000000000104', 1080, 1920, 30, 'h264', 'aac', 5000000, 'mp4')
ON CONFLICT DO NOTHING;

INSERT INTO video_variants (video_id, resolution, width, height, bitrate, codec, storage_key, manifest_path, status)
SELECT v.id, r.res::video_resolution, r.w, r.h, r.br, 'h264', 'hls/' || v.id || '/' || r.res, 'index.m3u8', 'READY'
FROM (VALUES ('00000000-0000-7000-8000-000000000101'::uuid), ('00000000-0000-7000-8000-000000000102'::uuid)) AS v(id)
CROSS JOIN (VALUES ('360p', 640, 360, 800000), ('720p', 1280, 720, 2800000), ('1080p', 1920, 1080, 5000000)) AS r(res, w, h, br)
ON CONFLICT DO NOTHING;

INSERT INTO video_thumbnails (video_id, storage_key, width, height, type, source, status, is_primary)
SELECT id, 'thumbs/' || id || '/poster.jpg', 1280, 720, 'POSTER', 'GENERATED', 'READY', true
FROM videos WHERE status = 'PUBLISHED'
ON CONFLICT DO NOTHING;

INSERT INTO video_tags (video_id, tag_id)
SELECT v.id, t.id FROM videos v CROSS JOIN tags t
WHERE v.id = '00000000-0000-7000-8000-000000000101' AND t.slug IN ('cinematic', 'neon', 'city')
ON CONFLICT DO NOTHING;

INSERT INTO video_processing_jobs (video_id, job_type, status, attempts, started_at, heartbeat_at, worker_id)
VALUES ('00000000-0000-7000-8000-000000000105', 'TRANSCODE', 'PROCESSING', 1, now(), now(), 'dev-worker-1')
ON CONFLICT DO NOTHING;

SELECT refresh_video_search(id) FROM videos WHERE status = 'PUBLISHED';

INSERT INTO subscriptions (subscriber_id, creator_id) VALUES
  ('00000000-0000-7000-8000-000000000020', '00000000-0000-7000-8000-000000000010')
ON CONFLICT DO NOTHING;

INSERT INTO video_reactions (user_id, video_id, reaction) VALUES
  ('00000000-0000-7000-8000-000000000020', '00000000-0000-7000-8000-000000000101', 'LIKE')
ON CONFLICT DO NOTHING;

INSERT INTO comments (id, video_id, video_creator_id, user_id, parent_comment_id, content, is_pinned) VALUES
  ('00000000-0000-7000-8000-000000000201', '00000000-0000-7000-8000-000000000101', '00000000-0000-7000-8000-000000000010',
   '00000000-0000-7000-8000-000000000010', NULL, 'Thanks for watching!', true),
  ('00000000-0000-7000-8000-000000000202', '00000000-0000-7000-8000-000000000101', '00000000-0000-7000-8000-000000000010',
   '00000000-0000-7000-8000-000000000020', NULL, 'Great quality, thanks for sharing.', false)
ON CONFLICT DO NOTHING;
INSERT INTO comments (id, video_id, video_creator_id, user_id, parent_comment_id, content) VALUES
  ('00000000-0000-7000-8000-000000000203', '00000000-0000-7000-8000-000000000101', '00000000-0000-7000-8000-000000000010',
   '00000000-0000-7000-8000-000000000010', '00000000-0000-7000-8000-000000000202', 'Glad you liked it!')
ON CONFLICT DO NOTHING;

INSERT INTO watch_history (user_id, video_id, last_position_seconds, watch_duration_seconds, completed) VALUES
  ('00000000-0000-7000-8000-000000000020', '00000000-0000-7000-8000-000000000101', 400, 400, false),
  ('00000000-0000-7000-8000-000000000020', '00000000-0000-7000-8000-000000000102', 1445, 1445, true)
ON CONFLICT DO NOTHING;

INSERT INTO watch_later (user_id, video_id) VALUES
  ('00000000-0000-7000-8000-000000000020', '00000000-0000-7000-8000-000000000102')
ON CONFLICT DO NOTHING;

INSERT INTO playlists (id, user_id, title, visibility) VALUES
  ('00000000-0000-7000-8000-000000000301', '00000000-0000-7000-8000-000000000020', 'Favorites', 'PRIVATE')
ON CONFLICT DO NOTHING;
INSERT INTO playlist_videos (playlist_id, video_id, position) VALUES
  ('00000000-0000-7000-8000-000000000301', '00000000-0000-7000-8000-000000000101', 0),
  ('00000000-0000-7000-8000-000000000301', '00000000-0000-7000-8000-000000000102', 1)
ON CONFLICT (playlist_id, video_id) DO NOTHING;   -- name the arbiter: a DEFERRABLE constraint cannot be inferred

INSERT INTO notifications (user_id, type, actor_user_id, video_id)
SELECT n.user_id::uuid, n.type::notification_type, n.actor::uuid, n.video::uuid
FROM (VALUES
  ('00000000-0000-7000-8000-000000000010', 'NEW_SUBSCRIBER', '00000000-0000-7000-8000-000000000020', NULL),
  ('00000000-0000-7000-8000-000000000020', 'NEW_VIDEO', '00000000-0000-7000-8000-000000000010', '00000000-0000-7000-8000-000000000101')
) AS n(user_id, type, actor, video)
WHERE NOT EXISTS (SELECT 1 FROM notifications);

COMMIT;
