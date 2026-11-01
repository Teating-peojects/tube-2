import { describe, expect, it } from "vitest";
import { parseSearchParams, toQueryString } from "./search-params";

const sp = (s: string) => new URLSearchParams(s);

describe("search params", () => {
  it("falls back to defaults for unknown values", () => {
    expect(parseSearchParams(sp("q=a&sort=hax&type=<script>&duration=9&date=x"))).toEqual({ q: "a", sort: "relevance", type: "all", duration: "any", date: "any" });
  });
  it("trims and caps the query", () => {
    expect(parseSearchParams(sp(`q=%20${"x".repeat(300)}%20`)).q).toHaveLength(100);
  });
  it("omits defaults when serialising", () => {
    expect(toQueryString({ q: "neon", sort: "relevance", type: "all", duration: "any", date: "any" })).toBe("q=neon");
    expect(toQueryString({ q: "neon", sort: "latest", type: "live", duration: "long", date: "week" })).toBe("q=neon&sort=latest&type=live&duration=long&date=week");
  });
});
