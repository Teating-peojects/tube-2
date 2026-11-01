export type NavItem = { label: string; href: string; icon: string };

export const mainNav: NavItem[] = [
  { label: "Home", href: "/", icon: "House" },
  { label: "Explore", href: "/explore", icon: "Compass" },
  { label: "Trending", href: "/trending", icon: "TrendUp" },
  { label: "Shorts", href: "/shorts", icon: "FilmStrip" },
  { label: "Live", href: "/live", icon: "Broadcast" },
  { label: "Subscriptions", href: "/subscriptions", icon: "SquaresFour" },
  { label: "History", href: "/history", icon: "ClockCounterClockwise" },
  { label: "Watch Later", href: "/watch-later", icon: "Clock" },
  { label: "Liked Videos", href: "/liked", icon: "Heart" },
  { label: "Playlists", href: "/playlists", icon: "ListPlus" },
];

export const creatorNav: NavItem[] = [
  { label: "My Videos", href: "/studio/videos", icon: "UploadSimple" },
  { label: "Analytics", href: "/studio/analytics", icon: "ChartBar" },
  { label: "Settings", href: "/settings", icon: "Gear" },
];

export const bottomNav: NavItem[] = [
  { label: "Home", href: "/", icon: "House" },
  { label: "Explore", href: "/explore", icon: "Compass" },
  { label: "Upload", href: "/upload", icon: "Plus" },
  { label: "Subs", href: "/subscriptions", icon: "SquaresFour" },
  { label: "Profile", href: "/profile", icon: "User" },
];

export const categories = ["All", "Trending", "New", "Popular", "Live", "Music", "Gaming", "Entertainment", "Shorts"];
