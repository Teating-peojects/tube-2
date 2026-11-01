"use client";
import { useId, useState, type FormEvent } from "react";

type Props = {
  onSubmit: (text: string) => Promise<unknown>;
  pending: boolean;
  error?: string | null;
  placeholder?: string;
  submitLabel?: string;
  autoFocus?: boolean;
  onCancel?: () => void;
};
const MAX = 2000;

export function CommentForm({ onSubmit, pending, error, placeholder = "Add a comment…", submitLabel = "Comment", autoFocus, onCancel }: Props) {
  const id = useId();
  const [text, setText] = useState("");
  const [focused, setFocused] = useState(Boolean(autoFocus));
  const valid = text.trim().length > 0 && text.length <= MAX;

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (!valid || pending) return;
    try { await onSubmit(text.trim()); setText(""); onCancel?.(); } catch { /* error surfaces via `error` prop; keep text */ }
  }

  return (
    <form onSubmit={submit} className="flex-1">
      <label className="sr-only" htmlFor={id}>{placeholder}</label>
      <textarea id={id} value={text} rows={focused ? 3 : 1} maxLength={MAX} autoFocus={autoFocus}
        onFocus={() => setFocused(true)} onChange={(e) => setText(e.target.value)} placeholder={placeholder}
        className="w-full resize-none border-b border-line bg-transparent py-2 text-sm outline-none transition-colors placeholder:text-fg-3 focus:border-white" />
      {error && <p role="alert" className="mt-1 text-xs text-accent">{error}</p>}
      {focused && (
        <div className="mt-2 flex items-center justify-end gap-2">
          <span className="mr-auto text-xs text-fg-3">{text.length}/{MAX}</span>
          <button type="button" onClick={() => { setText(""); setFocused(false); onCancel?.(); }} className="h-9 rounded-full px-4 text-sm font-semibold hover:bg-surface">Cancel</button>
          <button type="submit" disabled={!valid || pending} className="h-9 rounded-full bg-accent px-4 text-sm font-semibold hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-50">
            {pending ? "Posting…" : submitLabel}
          </button>
        </div>
      )}
    </form>
  );
}
