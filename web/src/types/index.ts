export type Creator = {
  id: string;
  name: string;
  avatarUrl: string | null;
  verified: boolean;
};

export type Video = {
  id: string;
  title: string;
  thumbnailUrl: string | null;
  durationSec: number;
  quality: "SD" | "HD" | "4K";
  views: number;
  publishedAt: string; // ISO date
  isLive: boolean;
  creator: Creator;
};

export type Paginated<T> = { items: T[]; nextCursor: string | null };

export type Reaction = "like" | "dislike" | null;
export type Caption = { lang: string; label: string; url: string };
export type ChannelSummary = Creator & { subscribers: number; isSubscribed: boolean };

export type VideoDetail = Omit<Video, "creator"> & {
  creator: ChannelSummary;
  description: string;
  streamUrl: string | null; // HLS (.m3u8) or progressive MP4, provided by backend
  posterUrl: string | null;
  captions: Caption[];
  likes: number;
  dislikes: number;
  reaction: Reaction;
  commentCount: number;
};

export type Comment = {
  id: string;
  author: { id: string; name: string; avatarUrl: string | null; isCreator: boolean };
  text: string; // plain text only; rendered as text, never as HTML
  createdAt: string;
  likes: number;
  liked: boolean;
  replyCount: number;
  pinned: boolean;
};
export type CommentSort = "top" | "new";

// ---- Search / discovery ----
export type SearchSort = "relevance" | "latest" | "views";
export type SearchType = "all" | "videos" | "channels" | "playlists" | "live";
export type SearchDuration = "any" | "short" | "medium" | "long";
export type SearchDate = "any" | "today" | "week" | "month" | "year";
export type SearchParams = { q: string; sort: SearchSort; type: SearchType; duration: SearchDuration; date: SearchDate };

export type ChannelResult = ChannelSummary & { videoCount: number; description: string };
export type PlaylistSummary = { id: string; title: string; videoCount: number; thumbnailUrl: string | null; creatorName: string };
export type SearchResult =
  | { kind: "video"; video: Video }
  | { kind: "channel"; channel: ChannelResult }
  | { kind: "playlist"; playlist: PlaylistSummary };
export type SearchPage = Paginated<SearchResult> & { total: number };

export type TrendingTab = "now" | "most-viewed" | "fast-growing" | "recent";

// ---- Auth ----
export type User = {
  id: string;
  username: string;
  displayName: string;
  email: string;
  avatarUrl: string | null;
  emailVerified: boolean;
  isCreator: boolean;
  joinedAt: string;
  bio?: string;
};

// ---- Library / channels / notifications ----
export type ChannelDetail = ChannelResult & {
  bannerUrl: string | null;
  joinedAt: string;
  totalViews: number;
  links: { label: string; url: string }[];
};
export type ChannelVideoKind = "videos" | "shorts" | "live";

export type Visibility = "public" | "private";
export type Playlist = {
  id: string; title: string; description: string; visibility: Visibility;
  videoCount: number; thumbnailUrl: string | null; ownerName: string; updatedAt: string;
};
export type PlaylistWithMembership = Playlist & { contains: boolean };
export type PlaylistDetail = Playlist & { isOwner: boolean; videos: Video[] };

export type HistoryItem = { video: Video; watchedAt: string; progressSec: number };

export type NotificationType =
  | "new_subscriber" | "new_comment" | "comment_reply" | "creator_upload"
  | "system" | "processing_completed" | "processing_failed";
export type AppNotification = { id: string; type: NotificationType; title: string; body: string; href: string; createdAt: string; read: boolean };

// ---- Upload & Studio ----
export type VideoVisibility = "public" | "unlisted" | "private";
export type UploadPhase = "idle" | "preparing" | "uploading" | "processing" | "encoding" | "ready" | "publishing" | "completed" | "failed" | "cancelled";
export type UploadInit = { uploadId: string; videoId: string; partSize: number; parts: { partNumber: number; url: string }[] };
export type ServerUploadStatus = { status: "processing" | "encoding" | "ready" | "published" | "failed"; progress: number; thumbnails: string[]; error?: string };
export type UploadMeta = {
  title: string; description: string; category: string; tags: string[]; visibility: VideoVisibility;
  comments: "on" | "off" | "review"; language: string; ageGate: boolean;
  thumbnail?: string; captions: { lang: string; assetId: string }[];
  /** Publish only: "everyone in the video is 18+, I have their consent and the rights". Must be true. */
  consent?: boolean;
};

export type StudioVideoStatus = "published" | "draft" | "processing" | "failed";
export type StudioFilter = "all" | "published" | "draft" | "processing" | "private" | "unlisted";
export type StudioVideo = {
  id: string; title: string; thumbnailUrl: string | null; durationSec: number; status: StudioVideoStatus;
  visibility: VideoVisibility; createdAt: string; views: number; comments: number; likes: number;
};
export type Period = "7d" | "28d" | "90d";
export type SeriesPoint = { date: string; value: number };
export type Stat = { label: string; value: number };
export type StudioOverview = {
  period: Period;
  totals: { views: number; watchTimeHours: number; subscribers: number; subscribersDelta: number; revenue: number | null };
  views: SeriesPoint[];
  topVideos: { id: string; title: string; views: number }[];
};
export type StudioAnalytics = {
  period: Period; views: SeriesPoint[]; watchTime: SeriesPoint[];
  audience: { countries: Stat[]; devices: Stat[] }; traffic: Stat[];
  engagement: { likes: number; comments: number; shares: number; avgViewDurationSec: number };
};
export type StudioComment = { id: string; videoId: string; videoTitle: string; author: string; text: string; createdAt: string; status: "published" | "held" };

// ---- Safety / settings ----
export type ReportTarget = { type: "video" | "comment" | "channel"; id: string };
export type ReportReason = { id: string; label: string; description?: string };
export type Settings = {
  privacy: { showSubscriptions: boolean; showLikedVideos: boolean; saveWatchHistory: boolean };
  notifications: { newVideo: boolean; replies: boolean; newSubscriber: boolean; system: boolean; email: boolean };
};
export type SettingsPatch = { [K in keyof Settings]?: Partial<Settings[K]> };
