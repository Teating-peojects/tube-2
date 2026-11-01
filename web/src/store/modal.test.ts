import { beforeEach, describe, expect, it } from "vitest";
import { modals, useModalStore } from "./modal";

describe("modal store", () => {
  beforeEach(() => useModalStore.setState({ modal: null }));

  it("resolves confirm(true) when answered yes", async () => {
    const p = modals.confirm({ title: "t", message: "m" });
    expect(useModalStore.getState().modal?.kind).toBe("confirm");
    useModalStore.getState().answer(true);
    await expect(p).resolves.toBe(true);
    expect(useModalStore.getState().modal).toBeNull();
  });
  it("treats dismissing as cancel", async () => {
    const p = modals.confirm({ title: "t", message: "m" });
    useModalStore.getState().close();
    await expect(p).resolves.toBe(false);
  });
  it("opens report and share modals", () => {
    modals.report({ type: "video", id: "v1" });
    expect(useModalStore.getState().modal).toEqual({ kind: "report", target: { type: "video", id: "v1" } });
    modals.share({ url: "https://x.test/a", title: "A" });
    expect(useModalStore.getState().modal?.kind).toBe("share");
  });
});
