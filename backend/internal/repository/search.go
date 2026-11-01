package repository

import (
	"context"
	"strings"
)

// EscapeLike escapes LIKE wildcards in user input (pattern uses backslash escape).
func EscapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// SearchVideos does full-text (video_search.document: title, description, tags,
// creator) plus partial-title matching over public videos. Ranked results use
// OFFSET pagination, bounded by the service layer. total is capped at 10,000.
func (c *Catalog) SearchVideos(ctx context.Context, q SearchVideosQuery) (rows []VideoRow, total int, err error) {
	if q.LiveOnly {
		return []VideoRow{}, 0, nil // no live streams exist yet
	}
	var a argList
	tsq := a.add(q.Q)
	contains := a.add("%" + EscapeLike(q.Q) + "%")

	where := []string{publicVideo, activeCreator, "(vsr.document @@ tsq.q OR v.title ILIKE " + contains + ")"}
	if q.MinDur != nil {
		where = append(where, "v.duration_seconds >= "+a.add(*q.MinDur))
	}
	if q.MaxDur != nil {
		where = append(where, "v.duration_seconds <= "+a.add(*q.MaxDur))
	}
	if q.Since != nil {
		where = append(where, "v.published_at >= "+a.add(*q.Since))
	}
	if q.Viewer != nil {
		where = append(where, viewerFilter(a.add(*q.Viewer)))
	}

	from := videoFrom + ` LEFT JOIN video_search vsr ON vsr.video_id = v.id CROSS JOIN LATERAL (SELECT plainto_tsquery('simple', ` + tsq + `) AS q) tsq`
	whereSQL := " WHERE " + strings.Join(where, " AND ")

	// Count first. Postgres rejects bind args a statement does not reference,
	// so the count gets only the args added so far.
	if err = c.pool.QueryRow(ctx, "SELECT count(*) FROM (SELECT 1 "+from+whereSQL+" LIMIT 10000) s", a.vals...).Scan(&total); err != nil {
		return nil, 0, err
	}

	var order string
	switch q.Sort {
	case "latest":
		order = "v.published_at DESC, v.id DESC"
	case "views":
		order = "vs.view_count DESC, v.id DESC"
	default:
		prefix := a.add(EscapeLike(q.Q) + "%")
		order = "(COALESCE(ts_rank_cd(vsr.document, tsq.q), 0) + CASE WHEN v.title ILIKE " + prefix + " THEN 0.5 ELSE 0 END) DESC, vs.view_count DESC, v.id DESC"
	}
	page := "SELECT " + videoCols + " " + from + whereSQL + " ORDER BY " + order +
		" LIMIT " + a.add(q.Limit) + " OFFSET " + a.add(q.Offset)
	r, err := c.pool.Query(ctx, page, a.vals...)
	if err != nil {
		return nil, 0, err
	}
	rows, err = collectVideos(r)
	return rows, total, err
}

func (c *Catalog) SearchChannels(ctx context.Context, viewer *string, q string, limit int) ([]ChannelRow, error) {
	esc := EscapeLike(q)
	rows, err := c.pool.Query(ctx, `SELECT `+channelCols+` `+channelFrom+`
WHERE `+activeCreator+` AND (u.display_name ILIKE $1 OR u.username::text ILIKE $1)
  AND ($4::uuid IS NULL OR NOT EXISTS (SELECT 1 FROM blocked_users bu WHERE bu.user_id = $4::uuid AND bu.blocked_user_id = u.id))
ORDER BY (u.display_name ILIKE $2) DESC, p.subscriber_count DESC, u.id LIMIT $3`, "%"+esc+"%", esc+"%", limit, viewer)
	if err != nil {
		return nil, err
	}
	return collectChannels(rows)
}

// SearchPlaylists matches PUBLIC playlists that contain at least one public video.
func (c *Catalog) SearchPlaylists(ctx context.Context, q string, limit int) ([]PlaylistRow, error) {
	esc := EscapeLike(q)
	rows, err := c.pool.Query(ctx, `SELECT `+playlistCols("NULL")+`
FROM playlists pl JOIN users ou ON ou.id = pl.user_id
WHERE pl.visibility = 'PUBLIC' AND pl.deleted_at IS NULL AND ou.status = 'ACTIVE' AND ou.deleted_at IS NULL
  AND (pl.title ILIKE $1 OR ou.display_name ILIKE $1)
  AND EXISTS (SELECT 1 FROM playlist_videos pv JOIN videos v ON v.id = pv.video_id JOIN users u ON u.id = v.creator_id WHERE pv.playlist_id = pl.id AND `+publicVideo+` AND `+activeCreator+`)
ORDER BY (pl.title ILIKE $2) DESC, pl.updated_at DESC, pl.id DESC LIMIT $3`, "%"+esc+"%", esc+"%", limit)
	if err != nil {
		return nil, err
	}
	return collectPlaylists(rows)
}

// SuggestTitles returns public video titles containing q (prefix matches first).
func (c *Catalog) SuggestTitles(ctx context.Context, q string, limit int) ([]string, error) {
	esc := EscapeLike(q)
	return c.strings(ctx, `SELECT v.title FROM videos v JOIN users u ON u.id = v.creator_id JOIN video_stats vs ON vs.video_id = v.id
WHERE `+publicVideo+` AND `+activeCreator+` AND v.title ILIKE $1
ORDER BY (v.title ILIKE $2) DESC, vs.view_count DESC, v.id LIMIT $3`, "%"+esc+"%", esc+"%", limit)
}

func (c *Catalog) SuggestChannels(ctx context.Context, q string, limit int) ([]string, error) {
	esc := EscapeLike(q)
	return c.strings(ctx, `SELECT u.display_name `+channelFrom+`
WHERE `+activeCreator+` AND u.display_name ILIKE $1
ORDER BY (u.display_name ILIKE $2) DESC, p.subscriber_count DESC, u.id LIMIT $3`, "%"+esc+"%", esc+"%", limit)
}

func (c *Catalog) strings(ctx context.Context, sql string, args ...any) ([]string, error) {
	rows, err := c.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
