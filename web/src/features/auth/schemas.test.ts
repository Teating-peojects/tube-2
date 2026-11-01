import { describe, expect, it } from "vitest";
import { uploadSchema } from "@/features/upload/schema";
import { loginSchema, registerSchema, resetSchema, verifySchema } from "./schemas";

const reg = { email: "a@b.co", username: "jane_doe", password: "Str0ngpassword", confirm: "Str0ngpassword", adult: true, terms: true };

describe("auth schemas", () => {
  it("accepts a valid registration", () => expect(registerSchema.safeParse(reg).success).toBe(true));
  it.each([
    ["weak password", { password: "short1", confirm: "short1" }],
    ["no digit", { password: "onlyletterslong", confirm: "onlyletterslong" }],
    ["mismatch", { confirm: "Different1234" }],
    ["bad username", { username: "bad name!" }],
    ["not 18+", { adult: false }],
    ["terms not accepted", { terms: false }],
    ["bad email", { email: "nope" }],
  ])("rejects %s", (_n, patch) => expect(registerSchema.safeParse({ ...reg, ...patch }).success).toBe(false));

  it("requires login fields", () => expect(loginSchema.safeParse({ identifier: "", password: "" }).success).toBe(false));
  it("validates the 6-digit code", () => {
    expect(verifySchema.safeParse({ code: "123456" }).success).toBe(true);
    expect(verifySchema.safeParse({ code: "12345a" }).success).toBe(false);
  });
  it("checks reset confirmation", () => expect(resetSchema.safeParse({ password: "Str0ngpassword", confirm: "x" }).success).toBe(false));
});

describe("upload schema", () => {
  const ok = { title: "Hello", description: "", category: "Music", tags: ["a"], visibility: "private", comments: "on", language: "en", ageGate: true, consent: true };
  it("accepts valid metadata", () => expect(uploadSchema.safeParse(ok).success).toBe(true));
  it("requires the adult/consent confirmation", () => expect(uploadSchema.safeParse({ ...ok, consent: false }).success).toBe(false));
  it("limits tags and title", () => {
    expect(uploadSchema.safeParse({ ...ok, tags: Array.from({ length: 16 }, (_, i) => `t${i}`) }).success).toBe(false);
    expect(uploadSchema.safeParse({ ...ok, title: " " }).success).toBe(false);
  });
});
