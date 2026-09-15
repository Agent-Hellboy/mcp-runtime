import type { ReactNode } from "react";

import { AccountBar } from "./AccountBar";
import { WorkspaceNavigation, type WorkspaceId } from "./WorkspaceNavigation";
import type { AuthStatus } from "../api/types";

type AppShellProps = {
  auth: AuthStatus;
  authBusy: boolean;
  workspace: WorkspaceId;
  onSelectWorkspace: (id: WorkspaceId) => void;
  onSignIn: () => void;
  onSignOut: () => void;
  children: ReactNode;
};

export function AppShell({
  auth,
  authBusy,
  workspace,
  onSelectWorkspace,
  onSignIn,
  onSignOut,
  children,
}: AppShellProps) {
  return (
    <div className="app-shell">
      <a className="skip-link" href="#workspace-content">
        Skip to content
      </a>
      <header className="app-header">
        <div className="app-header-identity">
          <p className="eyebrow">MCP Sentinel</p>
          <h1>Control Plane Dashboard</h1>
          <p className="lede">Monitor, govern, and operate your MCP infrastructure.</p>
        </div>
        <AccountBar status={auth} busy={authBusy} onSignIn={onSignIn} onSignOut={onSignOut} />
      </header>
      <WorkspaceNavigation
        active={workspace}
        onSelect={onSelectWorkspace}
        role={auth.authenticated ? auth.principal?.role : undefined}
      />
      <main className="workspace" id="workspace-content" tabIndex={-1}>
        {children}
      </main>
    </div>
  );
}
