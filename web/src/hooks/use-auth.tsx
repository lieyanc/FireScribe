import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { getAuthStatus, logout as apiLogout, UNAUTHENTICATED_EVENT, type AuthUser } from "../lib/api";

type AuthState =
  | { phase: "loading" }
  | { phase: "setup" }
  | { phase: "guest" }
  | { phase: "authenticated"; user: AuthUser };

type AuthContextValue = {
  state: AuthState;
  user: AuthUser | null;
  isAdmin: boolean;
  refresh: () => Promise<void>;
  signOut: () => Promise<void>;
};

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AuthState>({ phase: "loading" });

  const refresh = useCallback(async () => {
    try {
      const status = await getAuthStatus();
      if (status.needs_setup) {
        setState({ phase: "setup" });
      } else if (status.authenticated && status.user) {
        setState({ phase: "authenticated", user: status.user });
      } else {
        setState({ phase: "guest" });
      }
    } catch {
      setState({ phase: "guest" });
    }
  }, []);

  const signOut = useCallback(async () => {
    try {
      await apiLogout();
    } finally {
      setState({ phase: "guest" });
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  useEffect(() => {
    // Any API call answered with 401 (expired session, disabled account,
    // deleted user) drops the app back to the login screen.
    const onUnauthenticated = () => {
      setState((current) => (current.phase === "authenticated" ? { phase: "guest" } : current));
    };
    window.addEventListener(UNAUTHENTICATED_EVENT, onUnauthenticated);
    return () => window.removeEventListener(UNAUTHENTICATED_EVENT, onUnauthenticated);
  }, []);

  const value = useMemo<AuthContextValue>(
    () => ({
      state,
      user: state.phase === "authenticated" ? state.user : null,
      isAdmin: state.phase === "authenticated" && state.user.role === "admin",
      refresh,
      signOut,
    }),
    [state, refresh, signOut],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const context = useContext(AuthContext);
  if (!context) {
    throw new Error("useAuth must be used within AuthProvider");
  }
  return context;
}
