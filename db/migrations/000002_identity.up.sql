-- 000002 identity: users, profiles, settings, sessions, verification tokens, age verification.

CREATE TABLE users (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),  -- generate UUIDv7 in Go for index locality
  username      citext NOT NULL,
  email         citext NOT NULL,
  phone         text,
  password_hash text NOT NULL,                               -- argon2id (preferred) or bcrypt, never plaintext
  display_name  text NOT NULL,
  role          user_role   NOT NULL DEFAULT 'USER',
  status        user_status NOT NULL DEFAULT 'PENDING',
  is_verified   boolean NOT NULL DEFAULT false,              -- cached: email verified
  is_creator    boolean NOT NULL DEFAULT false,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  last_login_at timestamptz,
  deleted_at    timestamptz,
  CONSTRAINT users_username_key UNIQUE (username),
  CONSTRAINT users_email_key UNIQUE (email),
  CONSTRAINT users_phone_key UNIQUE (phone),                 -- NULLs are allowed many times
  CONSTRAINT users_username_format CHECK (username ~ '^[A-Za-z0-9_]{3,24}$'),
  CONSTRAINT users_email_format CHECK (char_length(email) <= 254 AND email ~ '^[^@[:space:]]+@[^@[:space:]]+\.[^@[:space:]]+$'),
  CONSTRAINT users_phone_e164 CHECK (phone IS NULL OR phone ~ '^\+[1-9][0-9]{6,14}$'),
  CONSTRAINT users_password_hash_format CHECK (password_hash ~ '^\$(argon2id|2[aby])\$'),
  CONSTRAINT users_display_name_len CHECK (char_length(btrim(display_name)) BETWEEN 2 AND 40),
  CONSTRAINT users_creator_flag CHECK (role <> 'CREATOR' OR is_creator),
  CONSTRAINT users_deleted_consistency CHECK ((status = 'DELETED') = (deleted_at IS NOT NULL))
);
-- Moderation/admin lists of problematic accounts only (tiny partial index).
CREATE INDEX users_attention ON users (status, created_at DESC) WHERE status <> 'ACTIVE';
-- Channel search (autocomplete / fuzzy) over active creators.
CREATE INDEX users_display_name_trgm ON users USING gin (display_name gin_trgm_ops) WHERE is_creator AND status = 'ACTIVE';

CREATE TABLE user_profiles (
  user_id            uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
  bio                text NOT NULL DEFAULT '',
  avatar_storage_key text,
  banner_storage_key text,
  website            text,
  location           text,
  social_links       jsonb NOT NULL DEFAULT '[]'::jsonb,     -- [{ "label": "...", "url": "https://..." }]
  subscriber_count   integer NOT NULL DEFAULT 0,             -- cached, maintained by trigger (see 000007)
  video_count        integer NOT NULL DEFAULT 0,             -- cached: public published videos
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT user_profiles_bio_len CHECK (char_length(bio) <= 300),
  CONSTRAINT user_profiles_website CHECK (website IS NULL OR (char_length(website) <= 300 AND website ~* '^https?://')),
  CONSTRAINT user_profiles_location_len CHECK (location IS NULL OR char_length(location) <= 100),
  CONSTRAINT user_profiles_social_links CHECK (jsonb_typeof(social_links) = 'array' AND jsonb_array_length(social_links) <= 10),
  CONSTRAINT user_profiles_counters CHECK (subscriber_count >= 0 AND video_count >= 0)
);

CREATE TABLE user_settings (
  user_id                    uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
  language                   text NOT NULL DEFAULT 'en',
  theme                      theme_pref NOT NULL DEFAULT 'DARK',
  autoplay                   boolean NOT NULL DEFAULT true,
  email_notifications        boolean NOT NULL DEFAULT false,
  push_notifications         boolean NOT NULL DEFAULT true,
  comment_notifications      boolean NOT NULL DEFAULT true,   -- replies + comments on my videos
  subscription_notifications boolean NOT NULL DEFAULT true,   -- new videos from creators I follow
  subscriber_notifications   boolean NOT NULL DEFAULT true,   -- someone subscribed to me
  system_notifications       boolean NOT NULL DEFAULT true,
  show_subscriptions         boolean NOT NULL DEFAULT false,  -- privacy
  show_liked_videos          boolean NOT NULL DEFAULT false,  -- privacy
  save_watch_history         boolean NOT NULL DEFAULT true,   -- privacy (also gates writes to watch_history)
  save_search_history        boolean NOT NULL DEFAULT true,   -- privacy (gates writes to search_history)
  extra                      jsonb NOT NULL DEFAULT '{}'::jsonb,  -- future flags without a migration
  created_at                 timestamptz NOT NULL DEFAULT now(),
  updated_at                 timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT user_settings_language CHECK (language ~ '^[a-z]{2,3}(-[A-Za-z0-9]{2,8})*$'),
  CONSTRAINT user_settings_extra CHECK (jsonb_typeof(extra) = 'object' AND octet_length(extra::text) <= 4096)
);

CREATE TABLE sessions (
  id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id            uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  session_token_hash bytea NOT NULL,                          -- SHA-256 of a 256-bit random token
  refresh_token_hash bytea,
  ip_prefix          cidr,                                    -- truncated (/24 IPv4, /56 IPv6): data minimisation
  user_agent         text,
  device_info        jsonb NOT NULL DEFAULT '{}'::jsonb,
  expires_at         timestamptz NOT NULL,
  created_at         timestamptz NOT NULL DEFAULT now(),
  last_used_at       timestamptz NOT NULL DEFAULT now(),
  revoked_at         timestamptz,
  CONSTRAINT sessions_token_key UNIQUE (session_token_hash),
  CONSTRAINT sessions_refresh_key UNIQUE (refresh_token_hash),
  CONSTRAINT sessions_token_len CHECK (octet_length(session_token_hash) = 32),
  CONSTRAINT sessions_refresh_len CHECK (refresh_token_hash IS NULL OR octet_length(refresh_token_hash) = 32),
  CONSTRAINT sessions_expiry CHECK (expires_at > created_at),
  CONSTRAINT sessions_ip_prefix CHECK (
    ip_prefix IS NULL
    OR (family(ip_prefix) = 4 AND masklen(ip_prefix) <= 24)
    OR (family(ip_prefix) = 6 AND masklen(ip_prefix) <= 56)
  ),
  CONSTRAINT sessions_ua_len CHECK (user_agent IS NULL OR char_length(user_agent) <= 512)
);
CREATE INDEX sessions_active_by_user ON sessions (user_id, last_used_at DESC) WHERE revoked_at IS NULL;
CREATE INDEX sessions_expires_at ON sessions (expires_at);      -- retention job

-- One table for email verification, phone OTP and password reset (identical shape, different purpose).
-- Lookup: email links carry "<id>.<secret>" -> SELECT by id, compare token_hash in constant time.
-- OTP: SELECT latest unconsumed row for (user_id, purpose). token_hash = HMAC-SHA256(pepper, purpose|user_id|token).
CREATE TABLE verification_tokens (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  purpose      verification_purpose NOT NULL,
  token_hash   bytea NOT NULL,
  target       text,                                          -- email/phone being verified (supports change of address)
  attempts     smallint NOT NULL DEFAULT 0,
  max_attempts smallint NOT NULL DEFAULT 5,
  expires_at   timestamptz NOT NULL,
  consumed_at  timestamptz,                                   -- set exactly once, atomically (see queries)
  created_at   timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT verification_tokens_hash_len CHECK (octet_length(token_hash) = 32),
  CONSTRAINT verification_tokens_attempts CHECK (attempts >= 0 AND max_attempts > 0 AND attempts <= max_attempts),
  CONSTRAINT verification_tokens_expiry CHECK (expires_at > created_at),
  CONSTRAINT verification_tokens_target CHECK (purpose = 'PASSWORD_RESET' OR target IS NOT NULL)
);
CREATE INDEX verification_tokens_open ON verification_tokens (user_id, purpose, created_at DESC) WHERE consumed_at IS NULL;
CREATE INDEX verification_tokens_expires_at ON verification_tokens (expires_at);   -- retention job

-- Minimum data only: outcome + opaque provider reference. Never store ID documents or birth dates here.
CREATE TABLE age_verifications (
  id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id            uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  status             age_verification_status NOT NULL DEFAULT 'PENDING',
  method             age_verification_method NOT NULL,
  provider           text,
  provider_reference text,
  verified_at        timestamptz,
  expires_at         timestamptz,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT age_verifications_verified CHECK (status <> 'VERIFIED' OR verified_at IS NOT NULL),
  CONSTRAINT age_verifications_provider CHECK (method NOT IN ('THIRD_PARTY', 'DOCUMENT') OR provider IS NOT NULL),
  CONSTRAINT age_verifications_ref_len CHECK (provider_reference IS NULL OR char_length(provider_reference) <= 200),
  CONSTRAINT age_verifications_expiry CHECK (expires_at IS NULL OR verified_at IS NULL OR expires_at > verified_at)
);
CREATE UNIQUE INDEX age_verifications_one_pending ON age_verifications (user_id) WHERE status = 'PENDING';
CREATE INDEX age_verifications_latest ON age_verifications (user_id, created_at DESC);
