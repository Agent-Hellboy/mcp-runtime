import type { AuthStatus } from "../api/types";
import { hasUserIdentity, isTenantUser } from "../api/types";

export type WorkspaceId = "servers" | "activity" | "keys" | "legacy";

export type WorkspaceTab = {
  id: WorkspaceId;
  label: string;
  description: string;
  // Optional visibility gate, evaluated against the authenticated principal.
  // Omitted means always visible, matching the Phase 2 behavior.
  visible?: (auth: AuthStatus) => boolean;
};

export const WORKSPACE_TABS: WorkspaceTab[] = [
  {
    id: "servers",
    label: "Servers",
    description: "Deployed MCP servers and their governed tool catalog.",
  },
  {
    id: "activity",
    label: "Activity",
    description: "Your MCP usage and team membership.",
    // Legacy data-user-only: tenant accounts only.
    visible: isTenantUser,
  },
  {
    id: "keys",
    label: "Keys",
    description: "Personal API keys for agents and CI jobs.",
    // Legacy data-auth-required + data-user-identity-required.
    visible: hasUserIdentity,
  },
  {
    id: "legacy",
    label: "More workspaces",
    description: "Analytics, teams, access control, and operations.",
  },
];

// visibleWorkspaceTabs applies the same role gating the legacy dashboard used,
// so navigation cannot offer a workspace the principal may not read.
export function visibleWorkspaceTabs(auth: AuthStatus): WorkspaceTab[] {
  return WORKSPACE_TABS.filter((tab) => !tab.visible || tab.visible(auth));
}

type WorkspaceNavigationProps = {
  active: WorkspaceId;
  auth: AuthStatus;
  onSelect: (id: WorkspaceId) => void;
};

export function WorkspaceNavigation({ active, auth, onSelect }: WorkspaceNavigationProps) {
  return (
    <nav className="workspace-nav" aria-label="Dashboard workspaces">
      <ul className="workspace-nav-list">
        {visibleWorkspaceTabs(auth).map((tab) => {
          const isActive = tab.id === active;
          return (
            <li key={tab.id}>
              <button
                type="button"
                className={isActive ? "workspace-tab active" : "workspace-tab"}
                aria-current={isActive ? "page" : undefined}
                title={tab.description}
                data-testid={`workspace-tab-${tab.id}`}
                onClick={() => onSelect(tab.id)}
              >
                {tab.label}
              </button>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}
