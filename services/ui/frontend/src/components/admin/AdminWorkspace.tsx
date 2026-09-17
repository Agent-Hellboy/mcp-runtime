import { useCallback, useState } from "react";

import { AccessControlPanel, type AccessSelection } from "./AccessControlPanel";
import { AccessDetail } from "./AccessDetail";
import { AdminGuard } from "./AdminGuard";
import { ADMIN_GROUPS, ADMIN_SECTIONS, adminSection, type AdminSectionId } from "./adminSections";
import { OperationsPanel } from "./OperationsPanel";
import { PlatformHealthPanel } from "./PlatformHealthPanel";
import { TeamsPanel } from "./TeamsPanel";
import { UsageAnalyticsPanel } from "./UsageAnalyticsPanel";
import { SelectField } from "../../ui/Field";
import type { AuthStatus } from "../../api/types";

export type { AdminSectionId };

type AdminWorkspaceProps = {
  auth: AuthStatus;
  onSignIn: () => void;
  // Supplied by the shell so the section is part of the URL. Standalone
  // renders fall back to local state.
  section?: AdminSectionId;
  onSectionChange?: (section: AdminSectionId) => void;
};

export function AdminWorkspace({ auth, onSignIn, section, onSectionChange }: AdminWorkspaceProps) {
  const [localSection, setLocalSection] = useState<AdminSectionId>("access");
  const active = section ?? localSection;
  const [namespace, setNamespace] = useState("");
  const [selection, setSelection] = useState<AccessSelection | null>(null);

  const select = useCallback(
    (next: AdminSectionId) => {
      setSelection(null);
      if (onSectionChange) {
        onSectionChange(next);
      } else {
        setLocalSection(next);
      }
    },
    [onSectionChange]
  );

  const current = adminSection(active);

  return (
    <AdminGuard auth={auth} onSignIn={onSignIn}>
      <div className="admin-layout">
        <nav className="admin-rail" aria-label="Administration sections">
          {ADMIN_GROUPS.map((group) => {
            const items = ADMIN_SECTIONS.filter((item) => item.group === group);
            if (items.length === 0) {
              return null;
            }
            return (
              <div className="admin-rail-group" key={group}>
                <h2>{group}</h2>
                <ul className="admin-rail-list">
                  {items.map((item) => (
                    <li key={item.id}>
                      <button
                        type="button"
                        className="admin-rail-item"
                        aria-current={item.id === active && !selection ? "page" : undefined}
                        data-testid={`admin-section-${item.id}`}
                        onClick={() => select(item.id)}
                      >
                        {item.label}
                      </button>
                    </li>
                  ))}
                </ul>
              </div>
            );
          })}
        </nav>

        <div className="admin-section-select">
          <SelectField
            label="Administration section"
            value={active}
            data-testid="admin-section-select"
            options={ADMIN_SECTIONS.map((item) => ({
              value: item.id,
              label: `${item.group} · ${item.label}`,
            }))}
            onChange={(event) => select(event.target.value as AdminSectionId)}
          />
        </div>

        <div>
          {selection ? (
            <AccessDetail
              selection={selection}
              onBack={() => setSelection(null)}
              sectionLabel={current.label}
            />
          ) : active === "access" ? (
            <AccessControlPanel
              namespace={namespace}
              onNamespaceChange={setNamespace}
              onSelect={setSelection}
              onSignIn={onSignIn}
            />
          ) : active === "teams" ? (
            <TeamsPanel onSignIn={onSignIn} />
          ) : active === "operations" ? (
            <OperationsPanel onSignIn={onSignIn} />
          ) : active === "analytics" ? (
            <UsageAnalyticsPanel onSignIn={onSignIn} />
          ) : (
            <PlatformHealthPanel onSignIn={onSignIn} />
          )}
        </div>
      </div>
    </AdminGuard>
  );
}
