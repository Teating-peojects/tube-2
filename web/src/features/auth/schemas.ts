import { z } from "zod";

// Client-side validation is for UX only. The backend must validate everything again.
const password = z.string()
  .min(10, "Use at least 10 characters")
  .max(128, "Use at most 128 characters")
  .regex(/[A-Za-z]/, "Include at least one letter")
  .regex(/\d/, "Include at least one number");

export const loginSchema = z.object({
  identifier: z.string().trim().min(3, "Enter your email or username").max(254),
  password: z.string().min(1, "Enter your password").max(128),
});

export const registerSchema = z.object({
  email: z.string().trim().email("Enter a valid email address").max(254),
  username: z.string().trim().min(3, "At least 3 characters").max(24, "At most 24 characters")
    .regex(/^[A-Za-z0-9_]+$/, "Letters, numbers and underscores only"),
  password,
  confirm: z.string(),
  adult: z.literal(true, { errorMap: () => ({ message: "You must be 18 or older" }) }),
  terms: z.literal(true, { errorMap: () => ({ message: "Please accept the Terms and Privacy Policy" }) }),
}).refine((v) => v.password === v.confirm, { path: ["confirm"], message: "Passwords don’t match" });

export const forgotSchema = z.object({ email: z.string().trim().email("Enter a valid email address").max(254) });

export const resetSchema = z.object({ password, confirm: z.string() })
  .refine((v) => v.password === v.confirm, { path: ["confirm"], message: "Passwords don’t match" });

export const verifySchema = z.object({ code: z.string().regex(/^\d{6}$/, "Enter the 6-digit code") });

export type LoginValues = z.infer<typeof loginSchema>;
export type RegisterValues = z.infer<typeof registerSchema>;
export type ForgotValues = z.infer<typeof forgotSchema>;
export type ResetValues = z.infer<typeof resetSchema>;
export type VerifyValues = z.infer<typeof verifySchema>;

export const playlistSchema = z.object({
  title: z.string().trim().min(1, "Give your playlist a name").max(100, "At most 100 characters"),
  visibility: z.enum(["public", "private"]),
});
export type PlaylistValues = z.infer<typeof playlistSchema>;

export const accountSchema = z.object({
  displayName: z.string().trim().min(2, "At least 2 characters").max(40, "At most 40 characters"),
  bio: z.string().max(300, "At most 300 characters"),
});
export const changePasswordSchema = z.object({ currentPassword: z.string().min(1, "Enter your current password").max(128), password, confirm: z.string() })
  .refine((v) => v.password === v.confirm, { path: ["confirm"], message: "Passwords don’t match" });
export type AccountValues = z.infer<typeof accountSchema>;
export type ChangePasswordValues = z.infer<typeof changePasswordSchema>;
