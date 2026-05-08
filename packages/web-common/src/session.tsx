"use client";

import {
  createContext, useCallback, useContext, useEffect, useMemo, useState,
  type ReactNode,
} from "react";
import { services, ApiError } from "./api";

export type Session = {
  accessToken: string;
  refreshToken: string;
  user: {
    id: string;
    tenant: string;
    email: string;
    full_name: string;
    role: string;
    permissions: string[];
  };
};

const STORAGE_KEY = "naditos.session";

type Ctx = {
  session: Session | null;
  loading: boolean;
  login: (email: string, password: string, tenant?: string) => Promise<void>;
  logout: () => Promise<void>;
  hasPerm: (perm: string) => boolean;
};

const SessionContext = createContext<Ctx | null>(null);

export function SessionProvider({ children }: { children: ReactNode }) {
  const [session, setSession] = useState<Session | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (typeof window === "undefined") return;
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (raw) {
      try { setSession(JSON.parse(raw)); } catch { /* ignore */ }
    }
    setLoading(false);
  }, []);

  const persist = useCallback((s: Session | null) => {
    setSession(s);
    if (typeof window === "undefined") return;
    if (s) window.localStorage.setItem(STORAGE_KEY, JSON.stringify(s));
    else   window.localStorage.removeItem(STORAGE_KEY);
  }, []);

  const login = useCallback(async (email: string, password: string, tenant?: string) => {
    const res = await services.auth("/v1/auth/login", {
      method: "POST",
      body: { email, password, tenant },
      tenant,
    }) as {
      access_token: string; refresh_token: string;
      user: Session["user"];
    };
    persist({
      accessToken: res.access_token,
      refreshToken: res.refresh_token,
      user: res.user,
    });
  }, [persist]);

  const logout = useCallback(async () => {
    if (session) {
      try {
        await services.auth("/v1/auth/logout", {
          method: "POST",
          body: { refresh_token: session.refreshToken },
          tenant: session.user.tenant,
        });
      } catch (e) {
        if (!(e instanceof ApiError)) throw e;
      }
    }
    persist(null);
  }, [session, persist]);

  const hasPerm = useCallback(
    (perm: string) => session?.user.permissions.includes(perm) ?? false,
    [session],
  );

  const ctx = useMemo<Ctx>(() => ({ session, loading, login, logout, hasPerm }),
    [session, loading, login, logout, hasPerm]);

  return <SessionContext.Provider value={ctx}>{children}</SessionContext.Provider>;
}

export function useSession(): Ctx {
  const c = useContext(SessionContext);
  if (!c) throw new Error("useSession must be inside <SessionProvider>");
  return c;
}
