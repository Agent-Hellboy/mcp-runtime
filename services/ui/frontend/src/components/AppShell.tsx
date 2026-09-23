import { useEffect, useState, type ReactNode } from "react";

import { visibleWorkspaceTabs, type WorkspaceId } from "./WorkspaceNavigation";
import { Button, IconButton } from "../ui/Button";
import { Icon } from "../ui/Icon";
import type { AuthStatus } from "../api/types";

type AppShellProps = {
  auth: AuthStatus;
  authBusy: boolean;
  theme: "dark" | "light";
  onToggleTheme: () => void;
  workspace: WorkspaceId;
  onSelectWorkspace: (id: WorkspaceId) => void;
  onSignIn: () => void;
  onSignOut: () => void;
  children: ReactNode;
};

function principalUsername(status: AuthStatus): string {
  const email = status.principal?.email?.trim();
  if (email) return email.split("@")[0];
  const subject = status.principal?.subject?.trim();
  return subject || "User";
}

export function AppShell({
  auth,
  authBusy,
  theme,
  onToggleTheme,
  workspace,
  onSelectWorkspace,
  onSignIn,
  onSignOut,
  children,
}: AppShellProps) {
  const [menuOpen, setMenuOpen] = useState(false);
  const tabs = visibleWorkspaceTabs(auth);
  const nextTheme = theme === "dark" ? "light" : "dark";
  // The compact menu is a navigation affordance, not state worth keeping: any
  // route change closes it.
  useEffect(() => {
    setMenuOpen(false);
  }, [workspace]);

  function navItem(id: WorkspaceId, label: string, icon: Parameters<typeof Icon>[0]["name"], title: string) {
    const active = id === workspace;
    return (
      <li key={id}>
        <button
          type="button"
          className="nav-item"
          aria-current={active ? "page" : undefined}
          title={title}
          data-testid={`workspace-tab-${id}`}
          onClick={() => onSelectWorkspace(id)}
        >
          <Icon name={icon} size={15} />
          {label}
        </button>
      </li>
    );
  }

  return (
    <div className="shell">
      <a className="skip-link" href="#workspace-content">
        Skip to content
      </a>

      <header className="topbar">
        <div className="topbar-inner">
          <span className="brand">
            <img className="brand-mark" src="/favicon.svg" alt="" />
            <span className="brand-name">MCP Runtime</span>
          </span>

          <nav className="primary-nav" aria-label="Primary">
            <ul className="primary-nav-list">
              {tabs.map((tab) => navItem(tab.id, tab.label, tab.icon, tab.description))}
            </ul>
          </nav>

          <div className="topbar-actions">
            {auth.authenticated ? (
              <span className="account-chip" data-testid="account-state">
                <span className="account-who" title={principalUsername(auth)}>{principalUsername(auth)}</span>
              </span>
            ) : (
              <span className="account-chip" data-testid="account-state">
                <span className="account-who">Signed out</span>
              </span>
            )}

            <span className="topbar-divider" aria-hidden="true" />

            <a
              className="icon-btn"
              href="https://mcpruntime.org/docs/"
              target="_blank"
              rel="noreferrer"
              aria-label="Documentation (opens in a new tab)"
              title="Documentation"
              data-testid="docs-link"
            >
              <Icon name="book" />
            </a>

            <button
              type="button"
              className="icon-btn"
              aria-label={`Switch to ${nextTheme} mode`}
              aria-pressed={theme === "light"}
              title={theme === "dark" ? "Dark" : "Light"}
              onClick={onToggleTheme}
              data-testid="theme-toggle"
            >
              <Icon name={theme === "dark" ? "moon" : "sun"} />
              <span className="visually-hidden">{theme === "dark" ? "Dark" : "Light"}</span>
            </button>

            {auth.authenticated ? (
              <Button
                variant="ghost"
                size="sm"
                icon="logout"
                onClick={onSignOut}
                disabled={authBusy}
                data-testid="logout-button"
              >
                {authBusy ? "Signing out…" : "Sign out"}
              </Button>
            ) : (
              <Button variant="primary" size="sm" icon="login" onClick={onSignIn} data-testid="signin-button">
                Sign in
              </Button>
            )}

            <IconButton
              icon={menuOpen ? "close" : "menu"}
              label={menuOpen ? "Close navigation menu" : "Open navigation menu"}
              className="nav-toggle"
              aria-expanded={menuOpen}
              aria-controls="mobile-nav"
              data-testid="nav-toggle"
              onClick={() => setMenuOpen((open) => !open)}
            />
          </div>
        </div>
      </header>

      <nav
        className={menuOpen ? "mobile-nav is-open" : "mobile-nav"}
        id="mobile-nav"
        aria-label="Primary (compact)"
        hidden={!menuOpen}
      >
        <ul className="mobile-nav-list">
          {tabs.map((tab) => (
            <li key={tab.id}>
              <button
                type="button"
                className="nav-item"
                aria-current={tab.id === workspace ? "page" : undefined}
                data-testid={`mobile-tab-${tab.id}`}
                onClick={() => onSelectWorkspace(tab.id)}
              >
                <Icon name={tab.icon} size={15} />
                {tab.label}
              </button>
            </li>
          ))}
        </ul>
      </nav>

      <main className="main" id="workspace-content" tabIndex={-1}>
        {children}
      </main>
    </div>
  );
}
