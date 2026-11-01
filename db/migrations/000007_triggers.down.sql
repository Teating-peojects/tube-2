DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY[
    'users', 'user_settings', 'age_verifications', 'categories', 'videos', 'video_metadata',
    'video_variants', 'upload_sessions', 'video_processing_jobs', 'video_reactions', 'playlists', 'reports'
  ] LOOP
    EXECUTE format('DROP TRIGGER IF EXISTS %I ON %I', t || '_set_updated_at', t);
  END LOOP;
END $$;
DROP TRIGGER IF EXISTS reports_priority ON reports;
DROP TRIGGER IF EXISTS video_tags_count ON video_tags;
DROP TRIGGER IF EXISTS comment_reactions_count ON comment_reactions;
DROP TRIGGER IF EXISTS comments_depth ON comments;
DROP TRIGGER IF EXISTS comments_counts ON comments;
DROP TRIGGER IF EXISTS video_reactions_count ON video_reactions;
DROP TRIGGER IF EXISTS subscriptions_count ON subscriptions;
DROP TRIGGER IF EXISTS videos_profile_video_count ON videos;
DROP TRIGGER IF EXISTS videos_immutable_creator ON videos;
DROP TRIGGER IF EXISTS videos_create_stats ON videos;
DROP TRIGGER IF EXISTS users_create_children ON users;
DROP FUNCTION IF EXISTS trg_reports_priority();
DROP FUNCTION IF EXISTS trg_video_tags_count();
DROP FUNCTION IF EXISTS trg_comment_reactions_count();
DROP FUNCTION IF EXISTS trg_comments_depth();
DROP FUNCTION IF EXISTS trg_comments_counts();
DROP FUNCTION IF EXISTS comment_is_counted(comment_status, timestamptz);
DROP FUNCTION IF EXISTS trg_video_reactions_count();
DROP FUNCTION IF EXISTS bump_video_reaction(uuid, reaction_type, integer);
DROP FUNCTION IF EXISTS trg_subscriptions_count();
DROP FUNCTION IF EXISTS trg_videos_profile_video_count();
DROP FUNCTION IF EXISTS video_counts_as_public(video_status, video_visibility, moderation_status, timestamptz);
DROP FUNCTION IF EXISTS trg_videos_immutable_creator();
DROP FUNCTION IF EXISTS trg_videos_create_stats();
DROP FUNCTION IF EXISTS trg_users_create_children();
ALTER TABLE video_stats   RESET (fillfactor, autovacuum_vacuum_scale_factor, autovacuum_analyze_scale_factor);
ALTER TABLE user_profiles RESET (fillfactor, autovacuum_vacuum_scale_factor);
