// Package media turns storage keys into public URLs and derives display
// values from video metadata.
package media

import "strings"

// URLs builds public asset URLs from a CDN base. With no base configured every
// URL is nil, which the frontend renders as a placeholder.
type URLs struct{ base string }

func New(cdnBase string) URLs { return URLs{base: strings.TrimRight(cdnBase, "/")} }

// Asset returns the public URL for a storage key (thumbnails, avatars, banners).
func (u URLs) Asset(key *string) *string {
	if u.base == "" || key == nil || *key == "" {
		return nil
	}
	s := u.base + "/" + strings.TrimLeft(*key, "/")
	return &s
}

// AssetString is Asset for non-null keys.
func (u URLs) AssetString(key string) *string { return u.Asset(&key) }

// Quality maps the source's short side (720 for 1280x720 and for 720x1280
// portrait) to the badge shown on cards.
func Quality(shortSide *int32) string {
	switch {
	case shortSide == nil:
		return "SD"
	case *shortSide >= 2160:
		return "4K"
	case *shortSide >= 720:
		return "HD"
	default:
		return "SD"
	}
}
