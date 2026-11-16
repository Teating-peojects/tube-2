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

describe("UploadProgress: offline and refused files", () => {
  it("says it is waiting for the connection and hides the speed while offline", () => {
    render(<UploadProgress {...props} phase="uploading" offline />);
    expect(screen.getByRole("status")).toHaveTextContent("Waiting for connection");
    expect(screen.getByText(/continues by itself when the connection is back/)).toBeInTheDocument();
    expect(screen.queryByText(/\/s •/)).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeInTheDocument(); // can still be cancelled
  });
  it("shows speed and time left while online", () => {
    render(<UploadProgress {...props} phase="uploading" />);
    expect(screen.getByText(/\/s • 5s left/)).toBeInTheDocument();
  });
  it("offers another file instead of a pointless retry when the server refused the file", async () => {
    const onReset = vi.fn();
    render(<UploadProgress {...props} onReset={onReset} phase="failed" canRetry={false} error="This file couldn’t be read as a video." />);
    expect(screen.queryByRole("button", { name: "Retry upload" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Cancel" })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Choose another file" }));
    expect(onReset).toHaveBeenCalled();
  });
  it("keeps the action buttons touch-sized", () => {
    render(<UploadProgress {...props} phase="failed" error="Connection lost." />);
    for (const name of ["Retry upload", "Cancel"]) expect(screen.getByRole("button", { name })).toHaveClass("h-11");
  });

  describe("cancelling processing", () => {
    it("offers it only while the server is processing or encoding", () => {
      const onCancelProcessing = vi.fn();
      const { rerender } = render(<UploadProgress {...props} phase="processing" onCancelProcessing={onCancelProcessing} />);
      expect(screen.getByRole("button", { name: "Cancel processing" })).toBeInTheDocument();
      rerender(<UploadProgress {...props} phase="encoding" onCancelProcessing={onCancelProcessing} />);
      expect(screen.getByRole("button", { name: "Cancel processing" })).toBeInTheDocument();
      for (const phase of ["uploading", "ready", "completed", "failed", "cancelled"] as const) {
        rerender(<UploadProgress {...props} phase={phase} onCancelProcessing={onCancelProcessing} />);
        expect(screen.queryByRole("button", { name: /Cancel processing|Cancelling/ })).not.toBeInTheDocument();
      }
    });
    it("is hidden when the page does not provide the action", () => {
      render(<UploadProgress {...props} phase="encoding" />);
      expect(screen.queryByRole("button", { name: "Cancel processing" })).not.toBeInTheDocument();
    });
    it("calls the action once and disables the button while it runs", async () => {
      let finish!: () => void;
      const onCancelProcessing = vi.fn(() => new Promise<void>((r) => { finish = r; }));
      render(<UploadProgress {...props} phase="processing" onCancelProcessing={onCancelProcessing} />);
      await userEvent.click(screen.getByRole("button", { name: "Cancel processing" }));
      const busy = screen.getByRole("button", { name: "Cancelling…" });
      expect(busy).toBeDisabled();
      await userEvent.click(busy); // a second click does nothing
      expect(onCancelProcessing).toHaveBeenCalledTimes(1);
      finish();
      expect(await screen.findByRole("button", { name: "Cancel processing" })).toBeEnabled();
    });
    it("tells the creator when cancelling failed", async () => {
      const onCancelProcessing = vi.fn().mockRejectedValue(new Error("network"));
      render(<UploadProgress {...props} phase="processing" onCancelProcessing={onCancelProcessing} />);
      await userEvent.click(screen.getByRole("button", { name: "Cancel processing" }));
      expect(await screen.findByRole("alert")).toHaveTextContent("Couldn't cancel processing");
    });
    it("shows the server's specific reason when processing failed", () => {
      render(<UploadProgress {...props} phase="failed" failedAtProcessing error="That video is longer than the maximum of 1 hour(s)" />);
      expect(screen.getByRole("alert")).toHaveTextContent("longer than the maximum");
    });
  });
});
