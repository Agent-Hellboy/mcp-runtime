import type { AuthStatus } from "../api/types";
import { hasUserIdentity, isAdmin, isTenantUser } from "../api/types";

export type WorkspaceId = "servers" | "admin" | "activity" | "keys" | "legacy";

export type WorkspaceTab = {
  id: WorkspaceId;
  label: string;
  description: string;
  // Optional visibility gate, evaluated against the authenticated principal.
  visible?: (auth: AuthStatus) => boolean;
};

export const WORKSPACE_TABS: WorkspaceTab[] = [
  {
    id: "servers",
    label: "Servers",
    description: "Deployed MCP servers and their governed tool catalog.",
  },
  {
    id: "admin",
    label: "Administration",
    description: "Access control, teams, operations, and platform health.",
    visible: isAdmin,
  },
  {
    id: "activity",
    label: "Activity",
    description: "Your MCP usage and team membership.",
    visible: isTenantUser,
  },
  {
    id: "keys",
    label: "Keys",
    description: "Personal API keys for agents and CI jobs.",
    visible: hasUserIdentity,
  },
  {
    id: "legacy",
    label: "More workspaces",
    description: "Analytics, teams, access control, and operations.",
  },
];

// Navigation fails closed: a tab is offered only when its server-backed
// principal is allowed to read it.
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
