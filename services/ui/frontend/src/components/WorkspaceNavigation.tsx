export type WorkspaceId = "servers" | "admin" | "legacy";

export type WorkspaceTab = {
  id: WorkspaceId;
  label: string;
  description: string;
  // Admin-only tabs are removed from the navigation for any other principal.
  adminOnly?: boolean;
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
    adminOnly: true,
  },
  {
    id: "legacy",
    label: "More workspaces",
    description: "Activity, keys, analytics, teams, access control, and operations.",
  },
];

// Navigation fails closed: a tab marked adminOnly is only listed when the
// server-returned principal role is exactly "admin".
export function visibleWorkspaceTabs(role: string | undefined): WorkspaceTab[] {
  return WORKSPACE_TABS.filter((tab) => !tab.adminOnly || role === "admin");
}

type WorkspaceNavigationProps = {
  active: WorkspaceId;
  onSelect: (id: WorkspaceId) => void;
  role?: string;
};

export function WorkspaceNavigation({ active, onSelect, role }: WorkspaceNavigationProps) {
  return (
    <nav className="workspace-nav" aria-label="Dashboard workspaces">
      <ul className="workspace-nav-list">
        {visibleWorkspaceTabs(role).map((tab) => {
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
