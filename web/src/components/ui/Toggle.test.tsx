import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { Toggle } from "./Toggle";

describe("Toggle", () => {
  it("is an accessible switch", async () => {
    const onChange = vi.fn();
    render(<Toggle label="Autoplay" description="Plays next" checked={false} onChange={onChange} />);
    const sw = screen.getByRole("switch", { name: "Autoplay" });
    expect(sw).toHaveAttribute("aria-checked", "false");
    expect(sw).toHaveAccessibleDescription("Plays next");
    await userEvent.click(sw);
    expect(onChange).toHaveBeenCalledWith(true);
  });
});
