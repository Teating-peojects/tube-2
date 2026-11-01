import { endpoints } from "@/config/endpoints";
import { env } from "@/config/env";
import type { Settings, SettingsPatch, User } from "@/types";
import { api } from "./api";
import { mockChangePassword, mockDeleteAccount, mockLogoutAll, mockUpdateAccount } from "./mock/auth";
import { mockGetSettings, mockPatchSettings } from "./mock/misc";

const send = (method: string, body?: unknown): RequestInit => ({ method, body: body === undefined ? undefined : JSON.stringify(body) });

export const getSettings = (): Promise<Settings> => (env.useMock ? mockGetSettings() : api(endpoints.settings));
export const patchSettings = (p: SettingsPatch): Promise<void> => (env.useMock ? mockPatchSettings(p) : api(endpoints.settings, send("PATCH", p)));
export const updateAccount = (v: { displayName: string; bio: string }): Promise<User> => (env.useMock ? mockUpdateAccount(v) : api(endpoints.account, send("PATCH", v)));
export const changePassword = (currentPassword: string, newPassword: string): Promise<void> =>
  env.useMock ? mockChangePassword(currentPassword, newPassword) : api(endpoints.auth.changePassword, send("POST", { currentPassword, newPassword }));
export const logoutAll = (): Promise<void> => (env.useMock ? mockLogoutAll() : api(endpoints.auth.logoutAll, send("POST")));
export const deleteAccount = (password: string): Promise<void> => (env.useMock ? mockDeleteAccount(password) : api(endpoints.accountDelete, send("POST", { password })));
