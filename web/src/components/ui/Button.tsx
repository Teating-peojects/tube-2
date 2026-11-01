import { SpinnerGap } from "@phosphor-icons/react/dist/ssr";
import type { ComponentProps } from "react";
import { cn } from "@/utils/format";

type Props = ComponentProps<"button"> & { variant?: "primary" | "secondary" | "ghost"; loading?: boolean; full?: boolean };

const variants = {
  primary: "bg-accent text-white hover:bg-accent-hover",
  secondary: "border border-line bg-surface text-white hover:bg-line",
  ghost: "text-white hover:bg-surface",
} as const;

export function Button({ variant = "primary", loading, full, className, children, disabled, type = "button", ...rest }: Props) {
  return (
    <button type={type} disabled={disabled || loading} aria-busy={loading || undefined}
      className={cn("inline-flex h-11 items-center justify-center gap-2 rounded-full px-6 text-sm font-semibold transition-colors disabled:cursor-not-allowed disabled:opacity-60",
        variants[variant], full && "w-full", className)}
      {...rest}>
      {loading && <SpinnerGap size={16} className="animate-spin" aria-hidden="true" />}
      {children}
    </button>
  );
}
