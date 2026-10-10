"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { api, type User } from "@/lib/api-client";

interface AuthState {
  user: User | null;
  isAdmin: boolean;
  loading: boolean;
  signOut: () => Promise<void>;
}

const AuthContext = React.createContext<AuthState | null>(null);

/** Fetches GET /api/auth/me on mount (docs/07 §4). `initialUser` skips the first flash. */
export function AuthProvider({
  children,
  initialUser = null,
}: {
  children: React.ReactNode;
  initialUser?: User | null;
}) {
  const router = useRouter();
  const [user, setUser] = React.useState<User | null>(initialUser);
  const [loading, setLoading] = React.useState(initialUser === null);

  React.useEffect(() => {
    let cancelled = false;
    api
      .me()
      .then((u) => !cancelled && setUser(u))
      .catch(() => !cancelled && setUser(null))
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
    };
  }, []);

  const signOut = React.useCallback(async () => {
    try {
      await api.logout();
    } finally {
      setUser(null);
      router.push("/login");
      router.refresh();
    }
  }, [router]);

  const value = React.useMemo<AuthState>(
    () => ({ user, isAdmin: user?.role === "admin", loading, signOut }),
    [user, loading, signOut],
  );
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthState {
  const ctx = React.useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within <AuthProvider>");
  return ctx;
}
