import { useState } from "react";

import { AdminGuard } from "./AdminGuard";
import { OperationsPanel } from "./OperationsPanel";
import { PlatformHealthPanel } from "./PlatformHealthPanel";
import { TeamsPanel } from "./TeamsPanel";
import { UsageAnalyticsPanel } from "./UsageAnalyticsPanel";
import type { AuthStatus } from "../../api/types";
import "../../styles/admin-workflows.css";

export type AdminSectionId = "teams" | "operations" | "platform" | "analytics";

type AdminSection = {
  id: AdminSectionId;
  label: string;
};

const SECTIONS: AdminSection[] = [
  { id: "teams", label: "Teams" },
  { id: "operations", label: "Operations" },
  { id: "platform", label: "Platform" },
  { id: "analytics", label: "Analytics" },
];

type AdminWorkspaceProps = {
  auth: AuthStatus;
  onSignIn: () => void;
};

// Access control (grants/sessions) is not here - it is any authenticated
// user's territory, not admin-only, so it lives in its own top-level
// AccessWorkspace. This workspace is strictly the admin-only surfaces:
// teams, operations, platform health, and usage analytics.
export function AdminWorkspace({ auth, onSignIn }: AdminWorkspaceProps) {
  const [section, setSection] = useState<AdminSectionId>("teams");

  return (
    <AdminGuard auth={auth} onSignIn={onSignIn}>
      <div className="admin-workspace">
        <nav className="admin-nav" aria-label="Administration sections">
          <ul className="admin-nav-list">
            {SECTIONS.map((item) => {
              const isActive = item.id === section;
              return (
                <li key={item.id}>
                  <button
                    type="button"
                    className={isActive ? "admin-tab active" : "admin-tab"}
                    aria-current={isActive ? "page" : undefined}
                    data-testid={`admin-section-${item.id}`}
                    onClick={() => setSection(item.id)}
                  >
                    {item.label}
                  </button>
                </li>
              );
            })}
          </ul>
        </nav>

        {section === "teams" ? (
          <TeamsPanel onSignIn={onSignIn} />
        ) : section === "operations" ? (
          <OperationsPanel onSignIn={onSignIn} />
        ) : section === "analytics" ? (
          <UsageAnalyticsPanel onSignIn={onSignIn} />
        ) : (
          <PlatformHealthPanel onSignIn={onSignIn} />
        )}
      </div>
    </AdminGuard>
  );
}
