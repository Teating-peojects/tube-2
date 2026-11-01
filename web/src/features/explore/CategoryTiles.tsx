import Link from "next/link";
import { categories } from "@/config/nav";
import { cn } from "@/utils/format";

const tint = ["#3a0a14", "#1c2430", "#2b1033", "#33200b", "#0f2b2b", "#1e1e2e", "#2a2a2e", "#3a0a14"];

export function CategoryTiles({ active }: { active: string }) {
  const list = categories.filter((c) => c !== "All");
  return (
    <nav aria-label="Browse categories">
      <ul className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4 2xl:grid-cols-8">
        {list.map((c, i) => {
          const on = active === c;
          return (
            <li key={c}>
              <Link href={on ? "/explore" : `/explore?category=${encodeURIComponent(c)}`} aria-current={on ? "true" : undefined}
                style={{ backgroundImage: `linear-gradient(135deg, ${tint[i % tint.length]}, #161618)` }}
                className={cn("flex h-20 items-end rounded-card border p-3 text-sm font-bold transition-colors",
                  on ? "border-accent" : "border-line hover:border-[#3a3a40]")}>
                {c}
              </Link>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}
