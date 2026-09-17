import { useMemo } from "react";

import type { AccessSelection } from "./AccessControlPanel";
import { sessionState } from "./AccessControlPanel";
import { StatusBadge, decisionTone } from "../../ui/Badge";
import { Button } from "../../ui/Button";
import { DataTable, buildColumns } from "../../ui/DataTable";
import { PageHeader } from "../../ui/PageHeader";
import { ErrorState, LoadingState } from "../../ui/States";
import { expiryState, formatAbsolute, formatTimestamp } from "../../lib/format";
import { useAccessActivity } from "../../hooks/useAdminData";
import { subjectLabel } from "../../api/types";
import type { GatewayEvent } from "../../api/types";

type AccessDetailProps = {
  selection: AccessSelection;
  onBack: () => void;
  sectionLabel: string;
};

const WEEK_MS = 7 * 24 * 60 * 60 * 1000;

function eventToolName(event: GatewayEvent): string {
  const payload = event.payload || {};
  return event.tool_name || (payload.tool_name as string) || (payload.rpc_method as string) || "—";
}

// Mirrors the legacy drill-down filtering from #378: a grant's activity is the
// last 7 days of decisions that matched it in the same namespace; a session's
// timeline is every decision recorded against it in that namespace.
export function filterGrantActivity(
  events: GatewayEvent[],
  name: string,
  namespace: string,
  now = Date.now()
): GatewayEvent[] {
  const since = now - WEEK_MS;
  return events.filter((event) => {
    const payload = event.payload || {};
    if (payload.matched_grant !== name) {
      return false;
    }
    const eventNamespace =
      (payload.matched_grant_namespace as string) || event.namespace || namespace;
    if (eventNamespace !== namespace) {
      return false;
    }
    const timestamp = Date.parse(event.timestamp || "");
    return Number.isNaN(timestamp) ? true : timestamp >= since;
  });
}

export function filterSessionTimeline(events: GatewayEvent[], namespace: string): GatewayEvent[] {
  return events.filter((event) => {
    const payload = event.payload || {};
    const eventNamespace =
      (payload.matched_session_namespace as string) || event.namespace || namespace;
    return eventNamespace === namespace;
  });
}

export function AccessDetail({ selection, onBack, sectionLabel }: AccessDetailProps) {
  const { kind, item } = selection;
  const isGrant = kind === "grant";
  const activityQuery = useAccessActivity(true, kind, item.name);

  const events = activityQuery.data ?? [];
  const rows = isGrant
    ? filterGrantActivity(events, item.name, item.namespace)
    : filterSessionTimeline(events, item.namespace);

  const columns = useMemo(
    () =>
      buildColumns<GatewayEvent>([
        {
          id: "time",
          header: "Time",
          rowHeader: true,
          sortValue: (event) => Date.parse(event.timestamp || "") || 0,
          cell: (event) => (
            <span title={formatAbsolute(event.timestamp)}>{formatTimestamp(event.timestamp)}</span>
          ),
        },
        { id: "tool", header: "Tool", sortValue: eventToolName, cell: eventToolName },
        {
          id: "decision",
          header: "Decision",
          sortValue: (event) => event.decision || "",
          cell: (event) => (
            <StatusBadge tone={decisionTone(event.decision)}>{event.decision || "unknown"}</StatusBadge>
          ),
        },
        {
          id: "reason",
          header: "Reason",
          cell: (event) => <span className="wrap-anywhere">{(event.payload?.reason as string) || "—"}</span>,
        },
      ]),
    []
  );

  const session = !isGrant ? (item as { revoked: boolean; expiresAt?: string; consentedTrust?: string }) : null;
  const grant = isGrant ? (item as { disabled: boolean; maxTrust?: string; allowedSideEffects?: string[] }) : null;

  return (
    <div data-testid="access-detail">
      <PageHeader
        title={item.name}
        breadcrumb={[
          { label: "Administration" },
          { label: sectionLabel, onClick: onBack },
          { label: item.name },
        ]}
        description={
          <span className="cell-code" data-testid="access-detail-kicker">
            {item.namespace} / {isGrant ? "MCPAccessGrant" : "MCPAgentSession"}
          </span>
        }
        actions={
          <Button variant="secondary" icon="chevronLeft" onClick={onBack} data-testid="access-detail-back">
            Back to access control
          </Button>
        }
      />

      <dl className="detail-grid" style={{ marginBottom: "var(--space-6)" }}>
        <div>
          <dt>Server</dt>
          <dd>{item.serverRef?.name || "—"}</dd>
        </div>
        <div>
          <dt>Subject</dt>
          <dd>{subjectLabel(item.subject)}</dd>
        </div>
        <div>
          <dt>{isGrant ? "Trust ceiling" : "Consented trust"}</dt>
          <dd>{(isGrant ? grant?.maxTrust : session?.consentedTrust) || "—"}</dd>
        </div>
        {isGrant ? (
          <div>
            <dt>Allowed side effects</dt>
            <dd>{(grant?.allowedSideEffects || []).join(", ") || "—"}</dd>
          </div>
        ) : (
          <div>
            <dt>Expires</dt>
            <dd title={formatAbsolute(session?.expiresAt)}>
              {expiryState(session?.expiresAt) === "none"
                ? "No expiry"
                : expiryState(session?.expiresAt) === "unparseable"
                  ? `Unreadable (${session?.expiresAt})`
                  : formatTimestamp(session?.expiresAt)}
            </dd>
          </div>
        )}
        <div>
          <dt>Status</dt>
          <dd>
            {isGrant ? (
              <StatusBadge tone={grant?.disabled ? "attention" : "ready"}>
                {grant?.disabled ? "Disabled" : "Active"}
              </StatusBadge>
            ) : (
              (() => {
                const state = sessionState(item as never);
                return <StatusBadge tone={state.tone}>{state.label}</StatusBadge>;
              })()
            )}
          </dd>
        </div>
      </dl>

      <section className="section">
        <div className="section-head">
          <h2 className="section-title" id="access-activity-title">
            {isGrant ? "Decisions in the last 7 days" : "Tool call timeline"}
          </h2>
          <p className="section-note">Gateway decision events come from the analytics service.</p>
        </div>

        {activityQuery.isPending ? (
          <LoadingState
            label={isGrant ? "Loading activity…" : "Loading timeline…"}
            testId="access-activity-loading"
          />
        ) : activityQuery.error ? (
          <ErrorState
            title={isGrant ? "Activity is unavailable." : "Timeline is unavailable."}
            detail="Gateway decision events come from the analytics service, which is not answering on this cluster. This is not the same as no activity."
            testId="access-activity-error"
          />
        ) : (
          <DataTable
            columns={columns}
            rows={rows}
            rowKey={(event) =>
              `${event.timestamp || ""}-${eventToolName(event)}-${event.decision || ""}`
            }
            caption="Gateway decisions recorded against this access record."
            regionLabel="Gateway decisions"
            testId="access-activity-table"
            emptyMessage={isGrant ? "No recent activity." : "No tool calls recorded."}
          />
        )}
      </section>
    </div>
  );
}
