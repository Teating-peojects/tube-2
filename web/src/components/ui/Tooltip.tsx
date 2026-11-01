"use client";
import { cloneElement, useId, type ReactElement } from "react";

/** Shows on hover and keyboard focus; the label is linked to the trigger with aria-describedby. */
export function Tooltip({ label, children }: { label: string; children: ReactElement<{ "aria-describedby"?: string }> }) {
  const id = useId();
  return (
    <span className="group/tt relative inline-flex">
      {cloneElement(children, { "aria-describedby": id })}
      <span id={id} role="tooltip"
        className="pointer-events-none absolute left-1/2 top-full z-50 mt-2 -translate-x-1/2 whitespace-nowrap rounded-md border border-line bg-surface px-2 py-1 text-xs opacity-0 shadow-soft transition-opacity group-focus-within/tt:opacity-100 group-hover/tt:opacity-100">
        {label}
      </span>
    </span>
  );
}
