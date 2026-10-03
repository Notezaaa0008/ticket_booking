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
  getToken,
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
  login: (email: string, password: string) => Promise<void>;
  register: (email: string, password: string, name: string) => Promise<void>;
  logout: () => void;
};

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const hasToken = useSyncExternalStore(subscribeAuthToken, hasAuthToken, () => false);
  const [user, setUser] = useState<User | null | undefined>(undefined);

  const state: AuthState = useMemo(() => {
    // hasToken comes from useSyncExternalStore(getToken): on the client it reflects localStorage on first paint.
    if (!hasToken) return { status: "anonymous" };
    if (user === undefined) return { status: "loading" };
    if (user === null) return { status: "anonymous" };
    return { status: "authenticated", user };
  }, [hasToken, user]);

  useEffect(() => {
    if (!hasToken) return;
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
  }, [hasToken]);

  const login = useCallback(async (email: string, password: string) => {
    const res = await apiLogin(email, password);
    setToken(res.token);
    setUser(res.user);
  }, []);

  const register = useCallback(
    async (email: string, password: string, name: string) => {
      await apiRegister(email, password, name);
      await login(email, password);
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

/** For protected pages: redirects anonymous visitors to /login and returns the auth state. */
export function useRequireAuth(): AuthState {
  const { state } = useAuth();
  const router = useRouter();

  useEffect(() => {
    if (typeof window === "undefined") return;
    if (!getToken()) {
      router.replace(`/login?next=${encodeURIComponent(window.location.pathname)}`);
      return;
    }
    if (state.status === "anonymous") {
      router.replace(`/login?next=${encodeURIComponent(window.location.pathname)}`);
    }
  }, [state.status, router]);

  return state;
}
