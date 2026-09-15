import { useState } from "react";

import { AccessControlPanel, type AccessSelection } from "./AccessControlPanel";
import { AccessDetail } from "./AccessDetail";
import { AdminGuard } from "./AdminGuard";
import { OperationsPanel } from "./OperationsPanel";
import { PlatformHealthPanel } from "./PlatformHealthPanel";
import { TeamsPanel } from "./TeamsPanel";
import type { AuthStatus } from "../../api/types";
import "../../styles/admin-workflows.css";

export type AdminSectionId = "access" | "teams" | "operations" | "platform";

type AdminSection = {
  id: AdminSectionId;
  label: string;
};

const SECTIONS: AdminSection[] = [
  { id: "access", label: "Access control" },
  { id: "teams", label: "Teams" },
  { id: "operations", label: "Operations" },
  { id: "platform", label: "Platform" },
];

type AdminWorkspaceProps = {
  auth: AuthStatus;
  onSignIn: () => void;
};

export function AdminWorkspace({ auth, onSignIn }: AdminWorkspaceProps) {
  const [section, setSection] = useState<AdminSectionId>("access");
  const [namespace, setNamespace] = useState("");
  const [selection, setSelection] = useState<AccessSelection | null>(null);

  return (
    <AdminGuard auth={auth} onSignIn={onSignIn}>
      <div className="admin-workspace">
        <nav className="admin-nav" aria-label="Administration sections">
          <ul className="admin-nav-list">
            {SECTIONS.map((item) => {
              const isActive = item.id === section && !selection;
              return (
                <li key={item.id}>
                  <button
                    type="button"
                    className={isActive ? "admin-tab active" : "admin-tab"}
                    aria-current={isActive ? "page" : undefined}
                    data-testid={`admin-section-${item.id}`}
                    onClick={() => {
                      setSection(item.id);
                      setSelection(null);
                    }}
                  >
                    {item.label}
                  </button>
                </li>
              );
            })}
          </ul>
        </nav>

        {selection ? (
          <AccessDetail selection={selection} onBack={() => setSelection(null)} />
        ) : section === "access" ? (
          <AccessControlPanel
            namespace={namespace}
            onNamespaceChange={setNamespace}
            onSelect={setSelection}
            onSignIn={onSignIn}
          />
        ) : section === "teams" ? (
          <TeamsPanel onSignIn={onSignIn} />
        ) : section === "operations" ? (
          <OperationsPanel onSignIn={onSignIn} />
        ) : (
          <PlatformHealthPanel onSignIn={onSignIn} />
        )}
      </div>
    </AdminGuard>
  );
}
