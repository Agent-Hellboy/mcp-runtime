import { useEffect, useState, type ReactNode } from "react";

import { visibleWorkspaceTabs, type WorkspaceId } from "./WorkspaceNavigation";
import { AccountMenu } from "./AccountMenu";
import { IconButton } from "../ui/Button";
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
            <img className="brand-logo" src={theme === "dark" ? "/brand/mcp-runtime-logo-dark.png" : "/brand/mcp-runtime-logo.png"} alt="MCP Runtime" />
          </span>

          <nav className="primary-nav" aria-label="Primary">
            <ul className="primary-nav-list">
              {tabs.map((tab) => navItem(tab.id, tab.label, tab.icon, tab.description))}
            </ul>
          </nav>

          <div className="topbar-actions">
            <AccountMenu
              auth={auth}
              authBusy={authBusy}
              workspace={workspace}
              onSignIn={onSignIn}
              onSignOut={onSignOut}
              onOpenCatalog={() => onSelectWorkspace("servers")}
            />

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
