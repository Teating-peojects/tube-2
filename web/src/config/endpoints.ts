// Single place to change backend routes.
const e = encodeURIComponent;
export const endpoints = {
  videos: "/api/v1/videos",
  video: (id: string) => `/api/v1/videos/${e(id)}`,
  reaction: (id: string) => `/api/v1/videos/${e(id)}/reaction`, // PUT { reaction: "like" | "dislike" | null }
  search: "/api/v1/search", // GET ?q&sort&type&duration&date&cursor
  searchSuggestions: "/api/v1/search/suggestions", // GET ?q
  trending: "/api/v1/trending", // GET ?tab&cursor
  popularCreators: "/api/v1/channels/popular",
  channel: (id: string) => `/api/v1/channels/${e(id)}`,
  subscription: (channelId: string) => `/api/v1/channels/${e(channelId)}/subscription`, // PUT { subscribed: boolean }
  comments: (videoId: string) => `/api/v1/videos/${e(videoId)}/comments`, // GET ?cursor&sort | POST { text, parentId? }
  commentReplies: (commentId: string) => `/api/v1/comments/${e(commentId)}/replies`,
  commentLike: (commentId: string) => `/api/v1/comments/${e(commentId)}/like`, // PUT { liked: boolean }
  shorts: "/api/v1/shorts", // GET ?cursor
  live: "/api/v1/live", // GET ?cursor
  reportReasons: "/api/v1/reports/reasons", // GET -> [{ id, label, description? }]
  reports: "/api/v1/reports", // POST { targetType, targetId, reason, details? }  (409 already_reported, 429 rate_limited)
  notInterested: (videoId: string) => `/api/v1/videos/${e(videoId)}/not-interested`, // POST
  blockChannel: (channelId: string) => `/api/v1/channels/${e(channelId)}/block`, // PUT | DELETE
  settings: "/api/v1/settings", // GET | PATCH (partial, nested by group)
  account: "/api/v1/account", // PATCH { displayName, bio } -> User
  accountDelete: "/api/v1/account/delete", // POST { password }
  sitemapVideos: "/api/v1/sitemap/videos", // GET ?limit -> [{ id, updatedAt }]
  age: {
    verify: "/api/v1/age/verify", // POST { confirmed: true } -> 204 + signed xh_age cookie (the only proof the API trusts)
    status: "/api/v1/age/status", // GET -> { verified }
  },
  videoView: (id: string) => `/api/v1/videos/${e(id)}/view`, // POST (no body) once per play; counted once per viewer per 30 min
  videoProgress: (id: string) => `/api/v1/videos/${e(id)}/progress`, // POST { positionSec, watchedSec } (signed in)
  auth: {
    login: "/api/v1/auth/login", // POST { identifier, password } -> User (sets httpOnly cookie)
    register: "/api/v1/auth/register", // POST { email, username, password } -> User
    logout: "/api/v1/auth/logout", // POST
    me: "/api/v1/auth/me", // GET -> User (401 when anonymous)
    forgot: "/api/v1/auth/forgot-password", // POST { email } (always 2xx)
    reset: "/api/v1/auth/reset-password", // POST { token, password }
    verify: "/api/v1/auth/verify", // POST { code } -> User
    resend: "/api/v1/auth/verify/resend", // POST
    changePassword: "/api/v1/auth/change-password", // POST { currentPassword, newPassword }
    logoutAll: "/api/v1/auth/logout-all", // POST
  },
  channelVideos: (id: string) => `/api/v1/channels/${e(id)}/videos`, // GET ?kind=videos|shorts|live&sort=latest|popular&cursor
  channelPlaylists: (id: string) => `/api/v1/channels/${e(id)}/playlists`,
  playlists: "/api/v1/playlists", // GET ?videoId= (mine, with `contains`) | POST { title, visibility, videoId? }
  playlist: (id: string) => `/api/v1/playlists/${e(id)}`, // GET | PATCH { title?, description?, visibility? } | DELETE
  playlistVideo: (id: string, videoId: string) => `/api/v1/playlists/${e(id)}/videos/${e(videoId)}`, // PUT | DELETE
  playlistOrder: (id: string) => `/api/v1/playlists/${e(id)}/order`, // PUT { videoIds: string[] }
  history: "/api/v1/history", // GET ?cursor | DELETE (clear all)
  historyItem: (videoId: string) => `/api/v1/history/${e(videoId)}`, // DELETE
  watchLater: "/api/v1/watch-later", // GET ?cursor
  watchLaterIds: "/api/v1/watch-later/ids", // GET -> string[]
  watchLaterItem: (videoId: string) => `/api/v1/watch-later/${e(videoId)}`, // PUT | DELETE
  liked: "/api/v1/liked", // GET ?cursor
  subscriptionChannels: "/api/v1/subscriptions/channels", // GET -> ChannelResult[]
  subscriptionFeed: "/api/v1/subscriptions/feed", // GET ?cursor
  notificationsRead: "/api/v1/notifications/read", // POST { ids?: string[], all?: boolean }
  notificationsUnread: "/api/v1/notifications/unread-count", // GET -> { count }
  upload: {
    init: "/api/v1/upload/init", // POST { filename, size, contentType } -> { uploadId, videoId, partSize, parts: [{ partNumber, url }] }
    complete: (id: string) => `/api/v1/upload/${e(id)}/complete`, // POST { parts: [{ partNumber, etag }] }
    abort: (id: string) => `/api/v1/upload/${e(id)}`, // DELETE
    status: (id: string) => `/api/v1/upload/${e(id)}/status`, // GET -> { status, progress, thumbnails, error? }
    assets: (id: string) => `/api/v1/upload/${e(id)}/assets`, // POST { kind: "thumbnail" | "caption", contentType, size, lang? } -> { assetId, url }
  },
  videoMetadata: (id: string) => `/api/v1/videos/${e(id)}/metadata`, // PATCH (save draft)
  videoPublish: (id: string) => `/api/v1/videos/${e(id)}/publish`, // POST
  studio: {
    overview: "/api/v1/studio/overview", // GET ?period=7d|28d|90d
    videos: "/api/v1/studio/videos", // GET ?filter&cursor
    video: (id: string) => `/api/v1/studio/videos/${e(id)}`, // PATCH { visibility } | DELETE
    analytics: "/api/v1/studio/analytics", // GET ?period
    comments: "/api/v1/studio/comments", // GET ?filter=all|held&cursor
    commentApprove: (id: string) => `/api/v1/studio/comments/${e(id)}/approve`, // POST
    commentDelete: (id: string) => `/api/v1/comments/${e(id)}`, // DELETE
  },
  notifications: "/api/v1/notifications",
} as const;
