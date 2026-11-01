"use client";
import { Eye, EyeSlash } from "@phosphor-icons/react";
import { useId, useState, type ComponentProps, type ReactNode } from "react";
import { cn } from "@/utils/format";

type Props = ComponentProps<"input"> & { label: string; error?: string; hint?: string; trailing?: ReactNode };

export function Input({ label, error, hint, trailing, id, className, ...rest }: Props) {
  const auto = useId();
  const iid = id ?? auto;
  const describedBy = error ? `${iid}-err` : hint ? `${iid}-hint` : undefined;
  return (
    <div>
      <label htmlFor={iid} className="mb-1.5 block text-sm font-medium">{label}</label>
      <div className="relative">
        <input id={iid} aria-invalid={error ? true : undefined} aria-describedby={describedBy}
          className={cn("h-12 w-full rounded-xl border bg-card px-4 text-sm outline-none transition-colors placeholder:text-fg-3 focus:border-white",
            error ? "border-accent" : "border-line", trailing ? "pr-12" : "", className)}
          {...rest} />
        {trailing && <div className="absolute inset-y-0 right-1 flex items-center">{trailing}</div>}
      </div>
      {error ? <p id={`${iid}-err`} role="alert" className="mt-1.5 text-xs text-accent">{error}</p>
        : hint ? <p id={`${iid}-hint`} className="mt-1.5 text-xs text-fg-3">{hint}</p> : null}
    </div>
  );
}

export function PasswordInput(props: Omit<Props, "type" | "trailing">) {
  const [show, setShow] = useState(false);
  return (
    <Input {...props} type={show ? "text" : "password"}
      trailing={
        <button type="button" onClick={() => setShow((s) => !s)} aria-label={show ? "Hide password" : "Show password"} aria-pressed={show}
          className="grid size-10 place-items-center rounded-full text-fg-2 hover:text-white">
          {show ? <EyeSlash size={18} /> : <Eye size={18} />}
        </button>
      } />
  );
}

type FieldProps = { label: string; error?: string; hint?: string };

export function Textarea({ label, error, hint, id, className, ...rest }: ComponentProps<"textarea"> & FieldProps) {
  const auto = useId();
  const iid = id ?? auto;
  const describedBy = error ? `${iid}-err` : hint ? `${iid}-hint` : undefined;
  return (
    <div>
      <label htmlFor={iid} className="mb-1.5 block text-sm font-medium">{label}</label>
      <textarea id={iid} aria-invalid={error ? true : undefined} aria-describedby={describedBy}
        className={cn("w-full resize-y rounded-xl border bg-card px-4 py-3 text-sm outline-none transition-colors placeholder:text-fg-3 focus:border-white", error ? "border-accent" : "border-line", className)}
        {...rest} />
      {error ? <p id={`${iid}-err`} role="alert" className="mt-1.5 text-xs text-accent">{error}</p>
        : hint ? <p id={`${iid}-hint`} className="mt-1.5 text-xs text-fg-3">{hint}</p> : null}
    </div>
  );
}

export function Select({ label, error, hint, id, className, children, ...rest }: ComponentProps<"select"> & FieldProps) {
  const auto = useId();
  const iid = id ?? auto;
  const describedBy = error ? `${iid}-err` : hint ? `${iid}-hint` : undefined;
  return (
    <div>
      <label htmlFor={iid} className="mb-1.5 block text-sm font-medium">{label}</label>
      <select id={iid} aria-invalid={error ? true : undefined} aria-describedby={describedBy}
        className={cn("h-12 w-full rounded-xl border bg-card px-3 text-sm outline-none focus:border-white", error ? "border-accent" : "border-line", className)} {...rest}>
        {children}
      </select>
      {error ? <p id={`${iid}-err`} role="alert" className="mt-1.5 text-xs text-accent">{error}</p>
        : hint ? <p id={`${iid}-hint`} className="mt-1.5 text-xs text-fg-3">{hint}</p> : null}
    </div>
  );
}
