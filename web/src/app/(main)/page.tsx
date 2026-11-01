import { ContinueWatching } from "@/features/videos/ContinueWatching";
import { VideoFeed } from "@/features/videos/VideoFeed";

export default function HomePage() {
  return (
    <>
      <h1 className="sr-only">Home</h1>
      <ContinueWatching />
      <VideoFeed />
    </>
  );
}
