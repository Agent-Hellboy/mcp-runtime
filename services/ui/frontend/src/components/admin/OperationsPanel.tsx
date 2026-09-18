import { useMemo, useState } from "react";

import { AsyncSection } from "./AsyncSection";
import { StatusBadge, outcomeTone } from "../../ui/Badge";
import { Button } from "../../ui/Button";
import { DataTable, buildColumns } from "../../ui/DataTable";
import { DetailSheet } from "../../ui/DetailSheet";
import { TextField } from "../../ui/Field";
import { FilterBar, FilterSummary, type FilterChip } from "../../ui/FilterBar";
import { MetricGrid } from "../../ui/MetricCard";
import { PageHeader } from "../../ui/PageHeader";
import { TabPanel, Tabs } from "../../ui/Tabs";
import { formatAbsolute, formatTimestamp } from "../../lib/format";
import { OPERATIONS_LIMIT, useAdminReload, useOperations } from "../../hooks/useAdminData";
import type { AuditLogEntry, ImageActivity, UserActivity } from "../../api/types";

type OperationsPanelProps = {
  onSignIn: () => void;
};

type OperationsTab = "users" | "audit" | "images";

const LOADED_NOTE = `loaded entries (the API returns at most ${OPERATIONS_LIMIT} per collection)`;

export function OperationsPanel({ onSignIn }: OperationsPanelProps) {
  const [tab, setTab] = useState<OperationsTab>("users");
  const [draft, setDraft] = useState({ user: "", since: "", until: "" });
  const [applied, setApplied] = useState({ user: "", since: "", until: "" });
  const [inspected, setInspected] = useState<AuditLogEntry | null>(null);
  const reload = useAdminReload();

  const operationsQuery = useOperations(true, applied);
  const operations = operationsQuery.data;
  const users = operations?.users ?? [];
  const audit = operations?.audit_logs ?? [];
  const images = operations?.images ?? [];

  const userColumns = useMemo(
    () =>
      buildColumns<UserActivity>([
        {
          id: "email",
          header: "User",
          rowHeader: true,
          sortValue: (user) => user.email,
          cell: (user) => (
            <>
              {user.email}
              <span className="cell-detail">{user.role || "no role"}</span>
            </>
          ),
        },
        {
          id: "namespace",
          header: "Namespace",
          sortValue: (user) => user.namespace || "",
          cell: (user) => user.namespace || "—",
        },
        { id: "logins", header: "Logins", numeric: true, sortValue: (user) => user.login_count ?? 0, cell: (user) => String(user.login_count ?? 0) },
        {
          id: "failures",
          header: "Failed actions",
          numeric: true,
          sortValue: (user) => user.failed_action_count ?? 0,
          cell: (user) =>
            (user.failed_action_count ?? 0) > 0 ? (
              <StatusBadge tone="warning">{String(user.failed_action_count)}</StatusBadge>
            ) : (
              "0"
            ),
        },
        { id: "keys", header: "API keys", numeric: true, sortValue: (user) => user.api_keys ?? 0, cell: (user) => String(user.api_keys ?? 0) },
        {
          id: "last",
          header: "Last activity",
          sortValue: (user) => Date.parse(user.last_activity_at || "") || 0,
          cell: (user) => (
            <span title={formatAbsolute(user.last_activity_at)}>{formatTimestamp(user.last_activity_at)}</span>
          ),
        },
      ]),
    []
  );

  const auditColumns = useMemo(
    () =>
      buildColumns<AuditLogEntry>([
        {
          id: "time",
          header: "Time",
          rowHeader: true,
          sortValue: (entry) => Date.parse(entry.created_at || "") || 0,
          cell: (entry) => (
            <span title={formatAbsolute(entry.created_at)}>{formatTimestamp(entry.created_at)}</span>
          ),
        },
        { id: "action", header: "Action", sortValue: (entry) => entry.action || "", cell: (entry) => entry.action || "—" },
        {
          id: "resource",
          header: "Resource",
          sortValue: (entry) => entry.resource || "",
          cell: (entry) => <span className="cell-code wrap-anywhere">{entry.resource || "—"}</span>,
        },
        {
          id: "identity",
          header: "Identity",
          sortValue: (entry) => entry.auth_identity || entry.user_id || "",
          cell: (entry) => entry.auth_identity || entry.user_id || "—",
        },
        {
          id: "status",
          header: "Status",
          sortValue: (entry) => entry.status || "",
          cell: (entry) => <StatusBadge tone={outcomeTone(entry.status)}>{entry.status || "unknown"}</StatusBadge>,
        },
        {
          id: "details",
          header: "Details",
          cell: (entry) => (
            <Button variant="ghost" size="sm" data-testid="audit-inspect" onClick={() => setInspected(entry)}>
              Inspect
            </Button>
          ),
        },
      ]),
    []
  );

  const imageColumns = useMemo(
    () =>
      buildColumns<ImageActivity>([
        {
          id: "image",
          header: "Image",
          rowHeader: true,
          sortValue: (image) => image.image_ref,
          cell: (image) => <span className="cell-code wrap-anywhere">{image.image_ref}</span>,
        },
        { id: "server", header: "Server", sortValue: (image) => image.server_name || "", cell: (image) => image.server_name || "—" },
        { id: "namespace", header: "Namespace", sortValue: (image) => image.namespace || "", cell: (image) => image.namespace || "—" },
        { id: "email", header: "By", sortValue: (image) => image.email || "", cell: (image) => image.email || "—" },
        { id: "action", header: "Action", sortValue: (image) => image.action || "", cell: (image) => image.action || "—" },
        {
          id: "status",
          header: "Status",
          sortValue: (image) => image.status || "",
          cell: (image) => <StatusBadge tone={outcomeTone(image.status)}>{image.status || "unknown"}</StatusBadge>,
        },
        {
          id: "time",
          header: "When",
          sortValue: (image) => Date.parse(image.created_at || "") || 0,
          cell: (image) => (
            <span title={formatAbsolute(image.created_at)}>{formatTimestamp(image.created_at)}</span>
          ),
        },
      ]),
    []
  );

  function apply() {
    setApplied({ user: draft.user.trim(), since: draft.since, until: draft.until });
  }

  const chips: FilterChip[] = [];
  if (applied.user) {
    chips.push({
      id: "user",
      label: "User",
      value: applied.user,
      onRemove: () => {
        setDraft((current) => ({ ...current, user: "" }));
        setApplied((current) => ({ ...current, user: "" }));
      },
    });
  }
  if (applied.since) {
    chips.push({
      id: "since",
      label: "From",
      value: applied.since,
      onRemove: () => {
        setDraft((current) => ({ ...current, since: "" }));
        setApplied((current) => ({ ...current, since: "" }));
      },
    });
  }
  if (applied.until) {
    chips.push({
      id: "until",
      label: "To",
      value: applied.until,
      onRemove: () => {
        setDraft((current) => ({ ...current, until: "" }));
        setApplied((current) => ({ ...current, until: "" }));
      },
    });
  }

  return (
    <>
      <PageHeader
        title="Operations"
        breadcrumb={[{ label: "Administration" }, { label: "Operations" }]}
        description="Platform users, the audit trail, and image publish activity."
        actions={
          <Button variant="secondary" icon="refresh" onClick={reload} data-testid="operations-refresh">
            Refresh
          </Button>
        }
      />

      <MetricGrid
        label="Operations summary"
        testId="operations-stats"
        metrics={[
          { label: "Users", value: operations ? users.length : undefined, icon: "users" },
          { label: "Audit entries", value: operations ? audit.length : undefined, icon: "clock", hint: "Loaded window" },
          { label: "Image events", value: operations ? images.length : undefined, icon: "inbox", hint: "Loaded window" },
        ]}
      />

      <FilterBar label="Filter operations" onSubmit={apply}>
        <TextField
          label="User"
          type="search"
          fieldClassName="grow"
          leadingIcon
          placeholder="Email address"
          value={draft.user}
          data-testid="operations-user-filter"
          onChange={(event) => setDraft({ ...draft, user: event.target.value })}
        />
        <TextField
          label="From"
          type="date"
          value={draft.since}
          data-testid="operations-since"
          onChange={(event) => setDraft({ ...draft, since: event.target.value })}
        />
        <TextField
          label="To"
          type="date"
          value={draft.until}
          data-testid="operations-until"
          onChange={(event) => setDraft({ ...draft, until: event.target.value })}
        />
        <Button type="submit" variant="primary" data-testid="operations-apply">
          Apply
        </Button>
      </FilterBar>

      <FilterSummary
        chips={chips}
        count={`${users.length} users · ${audit.length} audit entries · ${images.length} image events loaded`}
        onClear={() => {
          setDraft({ user: "", since: "", until: "" });
          setApplied({ user: "", since: "", until: "" });
        }}
        testId="operations-summary"
      />

      <AsyncSection
        query={operationsQuery}
        loadingLabel="Loading platform operations…"
        errorTitle="Platform operations could not be loaded."
        onRetry={reload}
        onSignIn={onSignIn}
        testId="operations"
      >
        <>
          <Tabs
          label="Operations sections"
            active={tab}
            onSelect={setTab}
            testIdPrefix="operations-tab"
            items={[
              { id: "users", label: "Users", count: users.length },
              { id: "audit", label: "Audit trail", count: audit.length },
              { id: "images", label: "Image activity", count: images.length },
            ]}
          />

          {tab === "users" ? (
            <TabPanel id="users">
              <DataTable
                columns={userColumns}
                rows={users}
                rowKey={(user) => user.id || user.email}
                caption="Platform users with role, namespace, login counts, and last activity."
                regionLabel="Platform users"
                testId="operations-users-table"
                emptyMessage="No users match these filters."
                pageNote={LOADED_NOTE}
              />
            </TabPanel>
          ) : tab === "audit" ? (
            <TabPanel id="audit">
              <DataTable
                columns={auditColumns}
                rows={audit}
                rowKey={(entry) =>
                  `${entry.created_at || ""}-${entry.action}-${entry.resource}-${entry.user_id || ""}`
                }
                caption="Platform audit entries with action, resource, identity, and status."
                regionLabel="Audit trail"
                testId="operations-audit-table"
                emptyMessage="No audit entries match these filters."
                pageNote={LOADED_NOTE}
              />
            </TabPanel>
          ) : (
            <TabPanel id="images">
              <DataTable
                columns={imageColumns}
                rows={images}
                rowKey={(image) => `${image.created_at || ""}-${image.image_ref}-${image.action}`}
                caption="Image publish and deployment activity with the user and resulting status."
                regionLabel="Image activity"
                testId="operations-images-table"
                emptyMessage="No image activity matches these filters."
                pageNote={LOADED_NOTE}
              />
            </TabPanel>
          )}
        </>
      </AsyncSection>

      {inspected ? (
        <DetailSheet
          title={inspected.action || "Audit entry"}
          eyebrow={formatTimestamp(inspected.created_at)}
          onClose={() => setInspected(null)}
          testId="audit-detail"
          closeTestId="audit-detail-close"
        >
          <dl className="detail-rows">
            <div>
              <dt>Resource</dt>
              <dd className="cell-code">{inspected.resource || "—"}</dd>
            </div>
            <div>
              <dt>Namespace</dt>
              <dd>{inspected.namespace || "—"}</dd>
            </div>
            <div>
              <dt>Status</dt>
              <dd>
                <StatusBadge tone={outcomeTone(inspected.status)}>{inspected.status || "unknown"}</StatusBadge>
              </dd>
            </div>
            <div>
              <dt>Identity</dt>
              <dd>{inspected.auth_identity || inspected.user_id || "—"}</dd>
            </div>
            <div>
              <dt>Source</dt>
              <dd>{inspected.source || "—"}</dd>
            </div>
            <div>
              <dt>Actor IP</dt>
              <dd>{inspected.actor_ip || "—"}</dd>
            </div>
            <div>
              <dt>Server</dt>
              <dd>{inspected.server_name || "—"}</dd>
            </div>
            <div>
              <dt>Image</dt>
              <dd className="cell-code">{inspected.image_ref || "—"}</dd>
            </div>
            <div>
              <dt>Recorded at</dt>
              <dd>{formatAbsolute(inspected.created_at)}</dd>
            </div>
            <div>
              <dt>Message</dt>
              <dd className="wrap-anywhere">{inspected.message || "—"}</dd>
            </div>
          </dl>
        </DetailSheet>
      ) : null}
    </>
  );
}
