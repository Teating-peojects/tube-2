DROP FUNCTION IF EXISTS expire_user_suspensions(integer);
DROP TRIGGER IF EXISTS videos_hash_blocklist ON videos;
DROP FUNCTION IF EXISTS trg_videos_hash_blocklist();
DROP INDEX IF EXISTS videos_by_content_hash;
ALTER TABLE videos DROP CONSTRAINT IF EXISTS videos_content_sha256_len, DROP COLUMN IF EXISTS content_sha256;
DROP TABLE IF EXISTS blocked_hashes;
DROP TABLE IF EXISTS upload_consents;
