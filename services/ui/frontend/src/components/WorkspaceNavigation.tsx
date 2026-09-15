export type WorkspaceId = "servers" | "legacy";

export type WorkspaceTab = {
  id: WorkspaceId;
  label: string;
  description: string;
};

export const WORKSPACE_TABS: WorkspaceTab[] = [
  {
    id: "servers",
    label: "Servers",
    description: "Deployed MCP servers and their governed tool catalog.",
  },
  {
    id: "legacy",
    label: "More workspaces",
    description: "Activity, keys, analytics, teams, access control, and operations.",
  },
];

type WorkspaceNavigationProps = {
  active: WorkspaceId;
  onSelect: (id: WorkspaceId) => void;
};

export function WorkspaceNavigation({ active, onSelect }: WorkspaceNavigationProps) {
  return (
    <nav className="workspace-nav" aria-label="Dashboard workspaces">
      <ul className="workspace-nav-list">
        {WORKSPACE_TABS.map((tab) => {
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
