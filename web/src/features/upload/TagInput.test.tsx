import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it } from "vitest";
import { TagInput } from "./TagInput";

function Harness({ initial = [] as string[] }) {
  const [v, setV] = useState(initial);
  return <TagInput value={v} onChange={setV} />;
}

describe("TagInput", () => {
  it("adds tags with Enter and comma, normalising them", async () => {
    render(<Harness />);
    const input = screen.getByLabelText("Tags");
    await userEvent.type(input, "#Neon{Enter}city,");
    expect(screen.getByText("neon")).toBeInTheDocument();
    expect(screen.getByText("city")).toBeInTheDocument();
  });
  it("ignores duplicates and removes tags", async () => {
    render(<Harness initial={["a"]} />);
    await userEvent.type(screen.getByLabelText("Tags"), "a{Enter}");
    expect(screen.getAllByText("a")).toHaveLength(1);
    await userEvent.click(screen.getByRole("button", { name: "Remove tag a" }));
    expect(screen.queryByText("a")).not.toBeInTheDocument();
  });
  it("stops at 15 tags", () => {
    render(<Harness initial={Array.from({ length: 15 }, (_, i) => `t${i}`)} />);
    expect(screen.getByLabelText("Tags")).toBeDisabled();
  });
});
