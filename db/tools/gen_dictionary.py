#!/usr/bin/env python3
"""Generates db/docs/DATA_DICTIONARY.md from db/migrations/*.up.sql (so it can never drift from the real schema).
Run: python3 db/tools/gen_dictionary.py   (or `make dictionary`)"""
import re, glob, os

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

PURPOSE = {
 'users': 'Accounts. One row per person; credentials, role, lifecycle status. Soft-deleted via `deleted_at`.',
 'user_profiles': 'Public channel profile (bio, images, links) plus cached `subscriber_count` / `video_count`.',
 'user_settings': 'Per-user preferences: notifications, privacy flags, playback, history switches.',
 'sessions': 'Login sessions. Only token hashes are stored; IP is truncated to a prefix.',
 'verification_tokens': 'One-time tokens for email verification, phone OTP and password reset (HMAC-hashed, single use).',
 'age_verifications': 'Outcome of 18+ verification (status/method/provider reference). No identity documents.',
 'categories': 'Curated video categories.',
 'tags': 'Normalised tags with a cached `usage_count`.',
 'videos': 'Core video record: metadata, lifecycle `status`, `visibility`, `moderation_status`, storage keys (no binary data).',
 'video_stats': 'Hot counters (views, likes, dislikes, comments, watch time) kept off the wide `videos` row.',
 'video_tags': 'Many-to-many link between videos and tags.',
 'video_metadata': 'Technical facts about the source file (resolution, codec, fps, bitrate). Enforces max 1080p.',
 'video_variants': 'Transcoded HLS renditions (360p–1080p) and their processing status.',
 'video_thumbnails': 'Generated and custom thumbnails; at most one primary per video.',
 'video_captions': 'Subtitle tracks (WebVTT) per language.',
 'upload_sessions': 'Multipart upload bookkeeping (provider upload id, part size/count, expiry).',
 'video_processing_jobs': 'Durable record of FFmpeg pipeline jobs (queue state, attempts, heartbeat).',
 'video_search': 'Denormalised full-text document per video (title, tags, creator, description) with a GIN index.',
 'watch_history': 'Per-user playback position and last-watched time (History, Continue watching).',
 'video_reactions': 'One like/dislike per user per video.',
 'comments': 'Threaded comments (one reply level), moderation status, cached like/reply counts.',
 'comment_reactions': 'One reaction per user per comment.',
 'subscriptions': 'Free follow relationship: subscriber -> creator.',
 'playlists': 'User playlists with visibility; soft-deleted.',
 'playlist_videos': 'Ordered playlist items (`position`, reorderable in one statement).',
 'watch_later': 'Per-user "Watch later" list.',
 'blocked_users': 'Creators/users a user has blocked.',
 'not_interested': 'Videos a user asked to see less of.',
 'notifications': 'In-app notifications; title/body are built at read time from type + payload.',
 'search_history': 'Recent searches per user (capped, expiring).',
 'reports': 'User reports against a video, comment or user; evidence-preserving (RESTRICT FKs).',
 'moderation_actions': 'Append-only log of moderator decisions.',
 'audit_logs': 'Append-only security/admin audit trail (no secrets, hashed IP).',
 'upload_consents': 'Append-only record that the uploader declared everyone is 18+, consented and rights are held (one per published video).',
 'blocked_hashes': 'SHA-256 of original files that must never be published again; a trigger removes matching videos.',
 'video_views': 'Raw view events, partitioned by month, retained ~90 days. No FKs by design.',
 'video_daily_analytics': 'Per-video daily rollup (views, watch time, likes, comments, unique viewers).',
 'creator_daily_analytics': 'Per-creator daily rollup for Studio overview.',
 'creator_daily_breakdown': 'Per-creator daily views by country / device / traffic source.',
}

def strip_inline(sql): return re.sub(r'--[^\n]*', '', sql)

files = [(os.path.basename(p), open(p, encoding='utf-8').read()) for p in sorted(glob.glob(f'{ROOT}/migrations/*.up.sql'))]
tables, order = {}, []

for fname, raw in files:
    lines = raw.split('\n')
    i = 0
    while i < len(lines):
        m = re.match(r'CREATE TABLE (\w+) \(', lines[i])
        if not m: i += 1; continue
        t = m.group(1); order.append(t)
        body = []
        i += 1
        while i < len(lines) and not re.match(r'^\)', lines[i]):
            body.append(lines[i]); i += 1
        part = lines[i].strip() if i < len(lines) else ''
        info = {'file': fname, 'cols': [], 'constraints': [], 'partition': part.replace(');', '').strip(' )')}
        j = 0
        while j < len(body):
            line = body[j]
            mm = re.match(r'^  (\w+)\s+(.*)$', line)
            if not mm: j += 1; continue
            name, rest = mm.groups()
            if name.upper() in ('CONSTRAINT', 'PRIMARY', 'UNIQUE', 'CHECK', 'FOREIGN'):
                text = rest if name.upper() != 'CONSTRAINT' else rest
                cname = rest.split()[0] if name.upper() == 'CONSTRAINT' else name
                block = [line]
                depth = line.count('(') - line.count(')')
                while depth > 0 and j + 1 < len(body):
                    j += 1; block.append(body[j]); depth += body[j].count('(') - body[j].count(')')
                flat = re.sub(r'\s+', ' ', strip_inline(' '.join(block))).strip().rstrip(',')
                info['constraints'].append((cname, flat))
                j += 1; continue
            comment = ''
            cm = re.search(r'--\s*(.*)$', rest)
            if cm: comment = cm.group(1).strip(); rest = rest[:cm.start()]
            rest = rest.strip().rstrip(',')
            tm = re.match(r'([a-z0-9_]+(?:\([^)]*\))?(?:\[\])?)\s*(.*)$', rest)
            typ, tail = (tm.group(1), tm.group(2)) if tm else (rest, '')
            default = re.search(r'DEFAULT\s+(.+?)(?:\s+(?:CHECK|REFERENCES|GENERATED|NOT NULL|NULL|UNIQUE|PRIMARY)\b|$)', tail)
            ref = re.search(r'REFERENCES\s+(\w+)\s*\((\w+)\)(?:\s+ON DELETE\s+(CASCADE|RESTRICT|SET NULL))?', tail)
            gen = re.search(r'GENERATED ALWAYS AS (\(.*\)) STORED', tail)
            info['cols'].append({
                'name': name, 'type': typ,
                'notnull': 'NOT NULL' in tail or 'PRIMARY KEY' in tail,
                'pk': 'PRIMARY KEY' in tail,
                'default': default.group(1).strip() if default else '',
                'ref': f"{ref.group(1)}({ref.group(2)})" + (f" ON DELETE {ref.group(3)}" if ref and ref.group(3) else '') if ref else '',
                'gen': gen.group(1) if gen else '',
                'comment': comment,
            })
            j += 1
        tables[t] = info
        i += 1

# indexes
all_sql = '\n'.join(strip_inline(r) for _, r in files)
indexes = {}
for m in re.finditer(r'CREATE (UNIQUE )?INDEX (\w+) ON (\w+)(?: USING (\w+))? \((.*?)\)(?:\s+WHERE (.*?))?;', all_sql, re.S):
    uniq, name, t, using, cols, where = m.groups()
    cols = re.sub(r'\s+', ' ', cols).strip()
    where = re.sub(r'\s+', ' ', where).strip() if where else ''
    indexes.setdefault(t, []).append((name, bool(uniq), using or 'btree', cols, where))

# triggers
triggers = {}
for m in re.finditer(r'CREATE TRIGGER (\w+)\s+(.*?)\s+ON (\w+)\s', all_sql, re.S):
    triggers.setdefault(m.group(3), []).append((m.group(1), re.sub(r'\s+', ' ', m.group(2)).strip()))
for m in re.finditer(r"ARRAY\[\s*((?:'[a-z_]+',?\s*)+)\]\s*LOOP", all_sql):
    for t in re.findall(r"'([a-z_]+)'", m.group(1)):
        triggers.setdefault(t, []).append((f'{t}_set_updated_at', 'BEFORE UPDATE (keeps updated_at current)'))

def esc(s): return s.replace('|', '\\|')

out = ['# REDXTUBE data dictionary', '',
       '> **Generated** from `db/migrations/*.up.sql` by `db/tools/gen_dictionary.py` (`make dictionary`). Do not edit by hand.', '',
       f'{len(tables)} tables. Column notes come from the comments in the migrations.', '', '## Tables', '']
for t in order:
    out.append(f'- [`{t}`](#{t}) — {PURPOSE.get(t, "")}')
out.append('')
for t in order:
    info = tables[t]
    out += [f'## {t}', '', PURPOSE.get(t, ''), '', f'*Created in `{info["file"]}`*' + (f' — **{info["partition"]}**' if info['partition'] else ''), '',
            '| Column | Type | Null | Default | References | Notes |', '|---|---|---|---|---|---|']
    for c in info['cols']:
        notes = c['comment']
        if c['gen']: notes = (notes + ' ' if notes else '') + f'*generated:* `{esc(c["gen"])}`'
        if c['pk']: notes = (notes + ' ' if notes else '') + '**PK**'
        out.append(f'| `{c["name"]}` | `{esc(c["type"])}` | {"no" if c["notnull"] else "yes"} | {"`" + esc(c["default"]) + "`" if c["default"] else ""} | {"`" + c["ref"] + "`" if c["ref"] else ""} | {esc(notes)} |')
    if info['constraints']:
        out += ['', '**Constraints**', '']
        for name, text in info['constraints']:
            out.append(f'- `{name}` — `{esc(text[:230])}`' if name not in ('PRIMARY', 'UNIQUE', 'CHECK', 'FOREIGN') else f'- `{esc(text[:230])}`')
    if indexes.get(t):
        out += ['', '**Indexes**', '']
        for name, uniq, using, cols, where in indexes[t]:
            out.append(f'- `{name}` — {"UNIQUE " if uniq else ""}{using} (`{esc(cols)}`)' + (f' WHERE `{esc(where)}`' if where else ''))
    if triggers.get(t):
        out += ['', '**Triggers**', '']
        for name, desc in triggers[t]:
            out.append(f'- `{name}` — {desc}')
    out.append('')

dest = f'{ROOT}/docs/DATA_DICTIONARY.md'
open(dest, 'w', encoding='utf-8').write('\n'.join(out))
print(f'wrote {dest}: {len(tables)} tables, {sum(len(v) for v in indexes.values())} indexes, {sum(len(v) for v in triggers.values())} triggers')
missing = [t for t in order if t not in PURPOSE]
if missing: print('WARNING: no purpose text for', missing)
