import { cn } from "@/utils/format";

export function Skeleton({ className }: { className?: string }) {
  return <div aria-hidden="true" className={cn("skeleton rounded-md", className)} />;
}
