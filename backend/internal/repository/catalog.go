package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Catalog struct{ pool *pgxpool.Pool }

func NewCatalog(pool *pgxpool.Pool) *Catalog { return &Catalog{pool: pool} }

// "Verified" creator badge = a creator whose email is verified (the schema has
// no separate badge column; see docs/db-adaptation.md).
const videoCols = `v.id::text, v.title, v.duration_seconds, LEAST(vm.width, vm.height), vs.view_count, v.published_at,
  u.id::text, u.display_name, p.avatar_storage_key, (u.is_creator AND u.is_verified), t.storage_key`

const videoFrom = `FROM videos v
  JOIN users u ON u.id = v.creator_id
  JOIN user_profiles p ON p.user_id = u.id
  JOIN video_stats vs ON vs.video_id = v.id
  LEFT JOIN video_metadata vm ON vm.video_id = v.id
  LEFT JOIN video_thumbnails t ON t.video_id = v.id AND t.is_primary AND t.status = 'READY'`

func scanVideo(r pgx.Row, extra ...any) (VideoRow, error) {
	var v VideoRow
	dst := append([]any{&v.ID, &v.Title, &v.DurationSec, &v.ShortSide, &v.Views, &v.PublishedAt,
		&v.CreatorID, &v.CreatorName, &v.AvatarKey, &v.Verified, &v.ThumbKey}, extra...)
	err := r.Scan(dst...)
	return v, err
}

func collectVideos(rows pgx.Rows) ([]VideoRow, error) {
	defer rows.Close()
	out := []VideoRow{}
	for rows.Next() {
		v, err := scanVideo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// viewerFilter hides channels the viewer blocked and videos they marked "not interested".
func viewerFilter(viewerArg string) string {
	return `NOT EXISTS (SELECT 1 FROM blocked_users bu WHERE bu.user_id = ` + viewerArg + `::uuid AND bu.blocked_user_id = v.creator_id)
  AND NOT EXISTS (SELECT 1 FROM not_interested ni WHERE ni.user_id = ` + viewerArg + `::uuid AND ni.video_id = v.id)`
}

type argList struct{ vals []any }

func (a *argList) add(v any) string {
	a.vals = append(a.vals, v)
	return "$" + strconv.Itoa(len(a.vals))
}

// ListVideos serves feeds, shorts and channel tabs with keyset pagination.
func (c *Catalog) ListVideos(ctx context.Context, q VideoListQuery) ([]VideoRow, error) {
	if q.Live != nil && *q.Live {
		return []VideoRow{}, nil // no live streams exist yet
	}
	var a argList
	where := []string{publicVideo, activeCreator}
	if q.Kind != "" {
		where = append(where, "v.video_type = "+a.add(q.Kind)+"::video_type")
	}
	if q.CategorySlug != "" {
		where = append(where, "v.category_id = (SELECT id FROM categories WHERE slug = "+a.add(q.CategorySlug)+")")
	}
	if q.CreatorID != "" {
		where = append(where, "v.creator_id = "+a.add(q.CreatorID)+"::uuid")
	} else if q.Viewer != nil {
		where = append(where, viewerFilter(a.add(*q.Viewer)))
	}

	var order string
	switch q.Order {
	case OrderPopular:
		order = "vs.view_count DESC, v.id DESC"
		if q.After != nil {
			where = append(where, fmt.Sprintf("(vs.view_count, v.id) < (%s, %s::uuid)", a.add(q.After.Num), a.add(q.After.ID)))
		}
	default:
		order = "v.published_at DESC, v.id DESC"
		if q.After != nil {
			where = append(where, fmt.Sprintf("(v.published_at, v.id) < (%s, %s::uuid)", a.add(q.After.Time), a.add(q.After.ID)))
		}
	}

	sql := "SELECT " + videoCols + " " + videoFrom + " WHERE " + strings.Join(where, " AND ") +
		" ORDER BY " + order + " LIMIT " + a.add(q.Limit)
	rows, err := c.pool.Query(ctx, sql, a.vals...)
	if err != nil {
		return nil, err
	}
	return collectVideos(rows)
}

// Trending returns a bounded, computed ranking. Scores use recent view events
// plus engagement, never total views alone (except the explicit most-viewed
// tab). B8 moves this behind a Redis cache.
func (c *Catalog) Trending(ctx context.Context, tab string, viewer *string, offset, limit int) ([]VideoRow, error) {
	const cte = `WITH w AS (
  SELECT video_id,
    count(*) FILTER (WHERE created_at > now() - interval '24 hours') AS v24,
    count(*) FILTER (WHERE created_at > now() - interval '48 hours' AND created_at <= now() - interval '24 hours') AS vprev,
    count(*) AS v7
  FROM video_views WHERE created_at > now() - interval '7 days' GROUP BY video_id
) `
	const score = `(COALESCE(w.v24,0) * 3 + COALESCE(w.v7,0) + vs.like_count * 2 + vs.comment_count * 3)`

	var extraWhere, order string
	switch tab {
	case "now":
		order = score + " DESC, vs.view_count DESC, v.id DESC"
	case "recent":
		extraWhere = " AND v.published_at > now() - interval '14 days'"
		order = score + " DESC, v.published_at DESC, v.id DESC"
	case "fast-growing":
		order = "((COALESCE(w.v24,0) + 1.0) / (COALESCE(w.vprev,0) + 1.0)) DESC, COALESCE(w.v24,0) DESC, vs.view_count DESC, v.id DESC"
	case "most-viewed":
		order = "vs.view_count DESC, v.id DESC"
	default:
		return nil, fmt.Errorf("unknown trending tab %q", tab)
	}

	args := []any{limit, offset}
	if viewer != nil {
		args = append(args, *viewer)
		extraWhere += " AND " + viewerFilter("$3")
	}
	sql := cte + "SELECT " + videoCols + " " + videoFrom + " LEFT JOIN w ON w.video_id = v.id" +
		" WHERE " + publicVideo + " AND " + activeCreator + " AND v.video_type = 'LONG'" + extraWhere +
		" ORDER BY " + order + " LIMIT $1 OFFSET $2"
	rows, err := c.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return collectVideos(rows)
}

// VideoDetail returns a watchable video: PUBLIC and UNLISTED by id, anything
// published only for its owner. Returns (nil, nil) when it does not exist or is not visible.
func (c *Catalog) VideoDetail(ctx context.Context, id string, viewerID *string) (*VideoDetailRow, error) {
	sql := `SELECT ` + videoCols + `,
  v.description, vs.like_count, vs.dislike_count, vs.comment_count, p.subscriber_count, v.hls_master_key
` + videoFrom + `
WHERE v.id = $1::uuid AND ` + watchable("$2")

	var d VideoDetailRow
	row := c.pool.QueryRow(ctx, sql, id, viewerID)
	v, err := scanVideo(row, &d.Description, &d.Likes, &d.Dislikes, &d.CommentCount, &d.Subscribers, &d.HLSKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.VideoRow = v

	rows, err := c.pool.Query(ctx, `SELECT language, COALESCE(label, language), storage_key FROM video_captions WHERE video_id = $1::uuid AND status = 'READY' ORDER BY language`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	d.Captions = []CaptionRow{}
	for rows.Next() {
		var cr CaptionRow
		if err := rows.Scan(&cr.Lang, &cr.Label, &cr.Key); err != nil {
			return nil, err
		}
		d.Captions = append(d.Captions, cr)
	}
	return &d, rows.Err()
}

func (c *Catalog) Sitemap(ctx context.Context, limit int) ([]SitemapRow, error) {
	rows, err := c.pool.Query(ctx, `SELECT v.id::text, GREATEST(v.updated_at, v.published_at)
FROM videos v JOIN users u ON u.id = v.creator_id
WHERE `+publicVideo+` AND `+activeCreator+`
ORDER BY v.published_at DESC, v.id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SitemapRow{}
	for rows.Next() {
		var s SitemapRow
		if err := rows.Scan(&s.ID, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
