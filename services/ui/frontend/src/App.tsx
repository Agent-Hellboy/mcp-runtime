import { useCallback, useEffect, useState } from "react";

import { AppShell } from "./components/AppShell";
import { LegacyWorkspace } from "./components/LegacyWorkspace";
import { SignInPanel } from "./components/SignInPanel";
import { ServersWorkspace } from "./components/servers/ServersWorkspace";
import { AdminWorkspace } from "./components/admin/AdminWorkspace";
import type { WorkspaceId } from "./components/WorkspaceNavigation";
import { login, logout, readAuthStatus, type LoginInput } from "./api/auth";
import { isAdmin, type AuthStatus } from "./api/types";

function loginErrorMessage(err: unknown): string {
  const raw = err instanceof Error ? err.message : "";
  if (!raw || raw.includes("unauthorized") || raw.includes("401")) {
    return "That email, password, or API key was not accepted.";
  }
  if (raw.includes("too_many_requests")) {
    return "Too many sign-in attempts. Wait a moment and try again.";
  }
  if (raw.includes("missing_credentials")) {
    return "Enter an email and password, or an API key.";
  }
  return "Sign-in failed. Try again.";
}

export function App() {
  const [auth, setAuth] = useState<AuthStatus>({ authenticated: false });
  const [authReady, setAuthReady] = useState(false);
  const [authBusy, setAuthBusy] = useState(false);
  const [showSignIn, setShowSignIn] = useState(false);
  const [loginError, setLoginError] = useState("");
  const [workspace, setWorkspace] = useState<WorkspaceId>("servers");

  useEffect(() => {
    let cancelled = false;
    readAuthStatus()
      .then((status) => {
        if (!cancelled) {
          setAuth(status);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setAuth({ authenticated: false });
        }
      })
      .finally(() => {
        if (!cancelled) {
          setAuthReady(true);
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // Second line of defence behind AdminGuard: if the session is not (or is no
  // longer) an admin, the admin workspace is not a reachable route at all.
  useEffect(() => {
    if (authReady && workspace === "admin" && !isAdmin(auth)) {
      setWorkspace("servers");
    }
  }, [authReady, workspace, auth]);

  const handleSignIn = useCallback(() => {
    setLoginError("");
    setShowSignIn(true);
    setWorkspace("servers");
  }, []);

  const handleSubmit = useCallback(async (input: LoginInput) => {
    setAuthBusy(true);
    setLoginError("");
    try {
      const status = await login(input);
      setAuth(status);
      setShowSignIn(false);
    } catch (err) {
      setLoginError(loginErrorMessage(err));
    } finally {
      setAuthBusy(false);
    }
  }, []);

  const handleSignOut = useCallback(async () => {
    setAuthBusy(true);
    try {
      await logout();
    } finally {
      setAuth({ authenticated: false });
      setShowSignIn(false);
      setAuthBusy(false);
    }
  }, []);

  const openLegacy = useCallback(() => {
    setShowSignIn(false);
    setWorkspace("legacy");
  }, []);

  let content;
  if (!authReady) {
    content = (
      <div className="state-block state-loading" role="status" aria-live="polite">
        <span className="state-spinner" aria-hidden="true" />
        <p className="state-title">Checking your session…</p>
      </div>
    );
  } else if (showSignIn && !auth.authenticated) {
    content = (
      <SignInPanel
        onSubmit={handleSubmit}
        onCancel={() => setShowSignIn(false)}
        onOpenLegacy={openLegacy}
        error={loginError}
        busy={authBusy}
      />
    );
  } else if (workspace === "legacy") {
    content = <LegacyWorkspace />;
  } else if (workspace === "admin") {
    content = <AdminWorkspace auth={auth} onSignIn={handleSignIn} />;
  } else {
    content = <ServersWorkspace authenticated={auth.authenticated} onSignIn={handleSignIn} />;
  }

  return (
    <AppShell
      auth={auth}
      authBusy={authBusy}
      workspace={workspace}
      onSelectWorkspace={(id) => {
        setShowSignIn(false);
        setWorkspace(id);
      }}
      onSignIn={handleSignIn}
      onSignOut={handleSignOut}
    >
      {content}
    </AppShell>
  );
}
