import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { UploadProgress } from "./UploadProgress";

const progress = { loaded: 50, total: 100, speed: 10, eta: 5 };
const props = { progress, serverProgress: 0, fileName: "clip.mp4", error: null, failedAtProcessing: false, onCancel: vi.fn(), onRetry: vi.fn(), onReset: vi.fn() };

describe("UploadProgress", () => {
  it("exposes progress to assistive tech", () => {
    render(<UploadProgress {...props} phase="uploading" />);
    expect(screen.getByRole("progressbar", { name: "Progress" })).toHaveAttribute("aria-valuenow", "50");
    expect(screen.getByRole("button", { name: "Cancel" })).toBeInTheDocument();
  });
  it("offers retry after a failed upload", async () => {
    const onRetry = vi.fn();
    render(<UploadProgress {...props} onRetry={onRetry} phase="failed" error="Connection lost." />);
    expect(screen.getByRole("alert")).toHaveTextContent("Connection lost.");
    await userEvent.click(screen.getByRole("button", { name: "Retry upload" }));
    expect(onRetry).toHaveBeenCalled();
  });
  it("asks to upload again when processing failed", () => {
    render(<UploadProgress {...props} phase="failed" failedAtProcessing error="Processing failed." />);
    expect(screen.getByRole("button", { name: "Upload again" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Retry upload" })).not.toBeInTheDocument();
  });
  it("shows the ready state", () => {
    render(<UploadProgress {...props} phase="ready" />);
    expect(screen.getByText("Ready to publish")).toBeInTheDocument();
  });
});
