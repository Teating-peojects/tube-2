import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { Modal } from "./Modal";

describe("Modal", () => {
  it("opens as a labelled dialog", () => {
    render(<Modal open onClose={() => {}} title="Share"><p>Body</p></Modal>);
    const dialog = screen.getByRole("dialog", { name: "Share", hidden: true });
    expect(dialog).toHaveAttribute("open");
    expect(screen.getByText("Body")).toBeInTheDocument();
  });
  it("closes with the close button", async () => {
    const onClose = vi.fn();
    render(<Modal open onClose={onClose} title="Share"><p>Body</p></Modal>);
    await userEvent.click(screen.getByRole("button", { name: "Close", hidden: true }));
    expect(onClose).toHaveBeenCalled();
  });
  it("closes on outside (backdrop) click but not on content click", async () => {
    const onClose = vi.fn();
    render(<Modal open onClose={onClose} title="Share"><p>Body</p></Modal>);
    await userEvent.click(screen.getByText("Body"));
    expect(onClose).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("dialog", { hidden: true }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
  it("locks page scroll while open and restores it", () => {
    const { rerender } = render(<Modal open onClose={() => {}} title="T"><p>x</p></Modal>);
    expect(document.documentElement.style.overflow).toBe("hidden");
    rerender(<Modal open={false} onClose={() => {}} title="T"><p>x</p></Modal>);
    expect(document.documentElement.style.overflow).toBe("");
  });
});
