"use client";

import { useRouter } from "next/navigation";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  useSyncExternalStore,
  type ReactNode,
} from "react";
import {
  ApiError,
  getMe,
  hasAuthToken,
  login as apiLogin,
  register as apiRegister,
  setToken,
  subscribeAuthToken,
  type User,
} from "@/lib/api";

export type AuthState =
  | { status: "loading" }
  | { status: "anonymous" }
  | { status: "authenticated"; user: User };

type AuthContextValue = {
  state: AuthState;
  login: (email: string, password: string) => Promise<User>;
  register: (email: string, password: string, name: string) => Promise<User>;
  logout: () => void;
};

const AuthContext = createContext<AuthContextValue | null>(null);

function subscribeClientReady(onChange: () => void): () => void {
  void onChange;
  return () => {};
}

function getClientReady(): boolean {
  return typeof window !== "undefined";
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const clientReady = useSyncExternalStore(subscribeClientReady, getClientReady, () => false);
  const hasToken = useSyncExternalStore(subscribeAuthToken, hasAuthToken, () => false);
  const [user, setUser] = useState<User | null | undefined>(undefined);

  const state: AuthState = useMemo(() => {
    // SSR/hydration: localStorage is unavailable on the server; stay loading until the client mounts.
    if (!clientReady) return { status: "loading" };
    if (!hasToken) {
      return { status: "anonymous" };
    }
    if (user === undefined) return { status: "loading" };
    if (user === null) return { status: "anonymous" };
    return { status: "authenticated", user };
  }, [clientReady, hasToken, user]);

  useEffect(() => {
    if (!clientReady || !hasToken) return;
    let cancelled = false;
    (async () => {
      try {
        const me = await getMe();
        if (!cancelled) setUser(me);
      } catch (e) {
        if (e instanceof ApiError && e.status === 401) setToken(null);
        if (!cancelled) setUser(null);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [clientReady, hasToken]);

  const login = useCallback(async (email: string, password: string) => {
    const res = await apiLogin(email, password);
    setToken(res.token);
    setUser(res.user);
    return res.user;
  }, []);

  const register = useCallback(
    async (email: string, password: string, name: string) => {
      await apiRegister(email, password, name);
      return login(email, password);
    },
    [login],
  );

  const logout = useCallback(() => {
    setToken(null);
    setUser(null);
  }, []);

  const value = useMemo(() => ({ state, login, register, logout }), [state, login, register, logout]);
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used inside AuthProvider");
  return ctx;
}

/** Only same-site relative paths are allowed as a post-login target (no open redirect). */
export function safeNext(next: string | null): string {
  return next && next.startsWith("/") && !next.startsWith("//") ? next : "/events";
}

/** Default app home: public catalog for guests and users; admin dashboard for admins. */
export function defaultHomeForUser(user: User | null | undefined): string {
  if (user?.role === "admin") return "/admin";
  return "/events";
}

/** Root `/` target once auth state is known (`null` while still loading). */
export function resolveRootRedirect(state: AuthState): string | null {
  if (state.status === "loading") return null;
  if (state.status === "authenticated") return defaultHomeForUser(state.user);
  return "/events";
}

/** Honors explicit `next` when safe; otherwise sends admins to /admin and users to /events. */
export function postLoginRedirect(user: User, nextParam: string | null): string {
  if (nextParam && nextParam.startsWith("/") && !nextParam.startsWith("//")) {
    return nextParam;
  }
  return defaultHomeForUser(user);
}

/** For protected pages: redirects anonymous visitors to /login and returns the auth state. */
export function useRequireAuth(): AuthState {
  const { state } = useAuth();
  const router = useRouter();

  useEffect(() => {
    if (state.status !== "anonymous") return;
    const next = encodeURIComponent(window.location.pathname + window.location.search);
    router.replace(`/login?next=${next}`);
  }, [state.status, router]);

  return state;
}
