package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

const channelCols = `u.id::text, u.display_name, p.avatar_storage_key, (u.is_creator AND u.is_verified), p.subscriber_count, p.bio,
  (SELECT count(*) FROM videos v WHERE v.creator_id = u.id AND ` + publicVideo + `)`

const channelFrom = `FROM user_profiles p JOIN users u ON u.id = p.user_id`

func scanChannel(r pgx.Row, extra ...any) (ChannelRow, error) {
	var c ChannelRow
	dst := append([]any{&c.ID, &c.Name, &c.AvatarKey, &c.Verified, &c.Subscribers, &c.Bio, &c.VideoCount}, extra...)
	return c, r.Scan(dst...)
}

func collectChannels(rows pgx.Rows) ([]ChannelRow, error) {
	defer rows.Close()
	out := []ChannelRow{}
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// PopularChannels lists active creators that have at least one public video.
func (c *Catalog) PopularChannels(ctx context.Context, viewer *string, limit int) ([]ChannelRow, error) {
	rows, err := c.pool.Query(ctx, `SELECT `+channelCols+` `+channelFrom+`
WHERE `+activeCreator+` AND EXISTS (SELECT 1 FROM videos v WHERE v.creator_id = u.id AND `+publicVideo+`)
  AND ($2::uuid IS NULL OR NOT EXISTS (SELECT 1 FROM blocked_users bu WHERE bu.user_id = $2::uuid AND bu.blocked_user_id = u.id))
ORDER BY p.subscriber_count DESC, u.id LIMIT $1`, limit, viewer)
	if err != nil {
		return nil, err
	}
	return collectChannels(rows)
}

// Channel returns (nil, nil) for unknown or inactive channels.
func (c *Catalog) Channel(ctx context.Context, id string) (*ChannelDetailRow, error) {
	var d ChannelDetailRow
	row := c.pool.QueryRow(ctx, `SELECT `+channelCols+`, p.banner_storage_key, u.created_at,
  (SELECT COALESCE(sum(vs.view_count), 0)::bigint FROM videos v JOIN video_stats vs ON vs.video_id = v.id WHERE v.creator_id = u.id AND `+publicVideo+`),
  p.website, p.social_links
`+channelFrom+` WHERE u.id = $1::uuid AND `+activeCreator, id)
	ch, err := scanChannel(row, &d.BannerKey, &d.JoinedAt, &d.TotalViews, &d.Website, &d.SocialLinks)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.ChannelRow = ch
	return &d, nil
}

// playlistCols counts and thumbnails only the videos listable for $viewer.
func playlistCols(viewerArg string) string {
	vis := listable(viewerArg)
	return `pl.id::text, pl.title, pl.description, pl.visibility::text,
  (SELECT count(*) FROM playlist_videos pv JOIN videos v ON v.id = pv.video_id JOIN users u ON u.id = v.creator_id WHERE pv.playlist_id = pl.id AND ` + vis + `),
  (SELECT t.storage_key FROM playlist_videos pv JOIN videos v ON v.id = pv.video_id JOIN users u ON u.id = v.creator_id
     JOIN video_thumbnails t ON t.video_id = v.id AND t.is_primary AND t.status = 'READY'
    WHERE pv.playlist_id = pl.id AND ` + vis + ` ORDER BY pv.position LIMIT 1),
  ou.display_name, pl.updated_at`
}

func collectPlaylists(rows pgx.Rows) ([]PlaylistRow, error) {
	defer rows.Close()
	out := []PlaylistRow{}
	for rows.Next() {
		var r PlaylistRow
		if err := rows.Scan(&r.ID, &r.Title, &r.Description, &r.Visibility, &r.VideoCount, &r.ThumbKey, &r.OwnerName, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ChannelPlaylists lists only PUBLIC playlists of an active channel (anonymous view).
func (c *Catalog) ChannelPlaylists(ctx context.Context, channelID string, limit int) ([]PlaylistRow, error) {
	rows, err := c.pool.Query(ctx, `SELECT `+playlistCols("NULL")+`
FROM playlists pl JOIN users ou ON ou.id = pl.user_id
WHERE pl.user_id = $1::uuid AND pl.visibility = 'PUBLIC' AND pl.deleted_at IS NULL AND ou.status = 'ACTIVE' AND ou.deleted_at IS NULL
ORDER BY pl.updated_at DESC, pl.id DESC LIMIT $2`, channelID, limit)
	if err != nil {
		return nil, err
	}
	return collectPlaylists(rows)
}
