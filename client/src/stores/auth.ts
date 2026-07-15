import { create } from "zustand";
import type { AuthUser, SignupPayload } from "@/types/auth";
import * as authApi from "@/services/auth";
import { eventStream } from "@/lib/event-stream";
import { resetSessionsStore } from "@/stores/sessions";
import { resetChatsStore } from "@/stores/chats";
import { resetCallsStore } from "@/stores/calls";
import { useOptionsStore } from "@/stores/options";
import { usePlanStore } from "@/stores/plan";

type State = {
  user: AuthUser | null;
  loading: boolean;
  needsSignup: boolean;
  refresh: () => Promise<void>;
  login: (email: string, password: string) => Promise<void>;
  signup: (payload: SignupPayload) => Promise<authApi.SignupResult>;
  verifyEmail: (email: string, code: string) => Promise<void>;
  logout: () => Promise<void>;
};

export const clearAuthClientState = (): void => {
  eventStream.close();
  resetSessionsStore();
  resetChatsStore();
  resetCallsStore();
  useOptionsStore.getState().reset();
  usePlanStore.getState().reset();
};

export const useAuth = create<State>((set) => ({
  user: null,
  loading: true,
  needsSignup: false,
  refresh: async () => {
    try {
      const r = await authApi.me();
      set({ user: r.user, needsSignup: !!r.needsSignup, loading: false });
    } catch {
      clearAuthClientState();
      set({ user: null, loading: false });
    }
  },
  login: async (email, password) => {
    clearAuthClientState();
    const u = await authApi.login(email, password);
    set({ user: u, needsSignup: false });
  },
  signup: async (payload) => {
    const r = await authApi.signup(payload);
    if ("needsVerification" in r && r.needsVerification) return r;
    set({ user: (r as { user: AuthUser }).user, needsSignup: false });
    return r;
  },
  verifyEmail: async (email, code) => {
    const u = await authApi.verifyEmail(email, code);
    set({ user: u, needsSignup: false });
  },
  logout: async () => {
    try {
      await authApi.logout();
    } finally {
      clearAuthClientState();
    }
    set({ user: null });
  },
}));

export const isAdmin = (u: AuthUser | null): boolean =>
  !!u && u.roles.includes("admin");