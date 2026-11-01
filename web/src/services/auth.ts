import { endpoints } from "@/config/endpoints";
import { env } from "@/config/env";
import type { User } from "@/types";
import { ApiError, api } from "./api";
import { mockForgot, mockLogin, mockLogout, mockMe, mockRegister, mockResend, mockReset, mockVerify } from "./mock/auth";

const post = <T>(path: string, body?: unknown) => api<T>(path, { method: "POST", body: body === undefined ? undefined : JSON.stringify(body) });

export const login = (identifier: string, password: string): Promise<User> =>
  env.useMock ? mockLogin(identifier, password) : post(endpoints.auth.login, { identifier, password });

export const register = (v: { email: string; username: string; password: string }): Promise<User> =>
  env.useMock ? mockRegister(v) : post(endpoints.auth.register, v);

export const logout = (): Promise<void> => (env.useMock ? mockLogout() : post(endpoints.auth.logout));

/** Returns null when anonymous. Other failures (network, 5xx) throw so we don't log users out by mistake. */
export async function getMe(): Promise<User | null> {
  try {
    return env.useMock ? await mockMe() : await api<User>(endpoints.auth.me);
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) return null;
    throw e;
  }
}

export const forgotPassword = (email: string): Promise<void> =>
  env.useMock ? mockForgot(email) : post(endpoints.auth.forgot, { email });

export const resetPassword = (token: string, password: string): Promise<void> =>
  env.useMock ? mockReset(token, password) : post(endpoints.auth.reset, { token, password });

export const verifyCode = (code: string): Promise<User> =>
  env.useMock ? mockVerify(code) : post(endpoints.auth.verify, { code });

export const resendCode = (): Promise<void> => (env.useMock ? mockResend() : post(endpoints.auth.resend));
