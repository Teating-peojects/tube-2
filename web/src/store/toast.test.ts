import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { toast, useToastStore } from "./toast";

describe("toast store", () => {
  beforeEach(() => { vi.useFakeTimers(); useToastStore.setState({ toasts: [] }); });
  afterEach(() => vi.useRealTimers());

  it("adds and auto-dismisses toasts", () => {
    toast.success("Saved");
    expect(useToastStore.getState().toasts).toHaveLength(1);
    vi.advanceTimersByTime(4001);
    expect(useToastStore.getState().toasts).toHaveLength(0);
  });
  it("keeps errors visible longer", () => {
    toast.error("Oops");
    vi.advanceTimersByTime(4001);
    expect(useToastStore.getState().toasts).toHaveLength(1);
    vi.advanceTimersByTime(2000);
    expect(useToastStore.getState().toasts).toHaveLength(0);
  });
  it("never shows more than three at once", () => {
    ["a", "b", "c", "d"].forEach((m) => toast.info(m));
    expect(useToastStore.getState().toasts.map((t) => t.message)).toEqual(["b", "c", "d"]);
  });
});
