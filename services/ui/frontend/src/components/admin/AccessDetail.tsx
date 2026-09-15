import { AdminTable, type AdminColumn } from "./AdminTable";
import type { AccessSelection } from "./AccessControlPanel";
import { ErrorState } from "../ErrorState";
import { LoadingState } from "../LoadingState";
import { StatusBadge } from "../StatusBadge";
import { useAccessActivity } from "../../hooks/useAdminData";
import { subjectLabel } from "../../api/types";
import type { GatewayEvent } from "../../api/types";

type AccessDetailProps = {
  selection: AccessSelection;
  onBack: () => void;
};

const WEEK_MS = 7 * 24 * 60 * 60 * 1000;

function eventToolName(event: GatewayEvent): string {
  const payload = event.payload || {};
  return (
    event.tool_name ||
    (payload.tool_name as string) ||
    (payload.rpc_method as string) ||
    "—"
  );
}

function eventTime(event: GatewayEvent): string {
  if (!event.timestamp) {
    return "—";
  }
  const parsed = new Date(event.timestamp);
  return Number.isNaN(parsed.getTime()) ? event.timestamp : parsed.toLocaleString();
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

export function filterSessionTimeline(
  events: GatewayEvent[],
  namespace: string
): GatewayEvent[] {
  return events.filter((event) => {
    const payload = event.payload || {};
    const eventNamespace =
      (payload.matched_session_namespace as string) || event.namespace || namespace;
    return eventNamespace === namespace;
  });
}

export function AccessDetail({ selection, onBack }: AccessDetailProps) {
  const { kind, item } = selection;
  const isGrant = kind === "grant";
  const activityQuery = useAccessActivity(true, kind, item.name);

  const events = activityQuery.data ?? [];
  const rows = isGrant
    ? filterGrantActivity(events, item.name, item.namespace)
    : filterSessionTimeline(events, item.namespace);

  const columns: Array<AdminColumn<GatewayEvent>> = [
    { id: "time", header: "Time", rowHeader: true, cell: eventTime },
    { id: "tool", header: "Tool", cell: eventToolName },
    {
      id: "decision",
      header: "Decision",
      cell: (event) => (
        <StatusBadge tone={event.decision === "deny" ? "attention" : "ready"}>
          {event.decision || "—"}
        </StatusBadge>
      ),
    },
    {
      id: "reason",
      header: "Reason",
      cell: (event) => ((event.payload?.reason as string) || "—"),
    },
  ];

  return (
    <section className="panel" aria-labelledby="access-detail-title" data-testid="access-detail">
      <div className="panel-head">
        <div>
          <p className="eyebrow" data-testid="access-detail-kicker">
            {item.namespace} / {isGrant ? "MCPAccessGrant" : "MCPAgentSession"}
          </p>
          <h2 id="access-detail-title">{item.name}</h2>
        </div>
        <button
          type="button"
          className="button ghost"
          onClick={onBack}
          data-testid="access-detail-back"
        >
          Back to access control
        </button>
      </div>

      <dl className="detail-grid">
        <div>
          <dt>Server</dt>
          <dd>{item.serverRef?.name || "—"}</dd>
        </div>
        <div>
          <dt>Subject</dt>
          <dd>{subjectLabel(item.subject)}</dd>
        </div>
        <div>
          <dt>{isGrant ? "Max trust" : "Consented trust"}</dt>
          <dd>
            {isGrant
              ? (selection.item as { maxTrust?: string }).maxTrust || "—"
              : (selection.item as { consentedTrust?: string }).consentedTrust || "—"}
          </dd>
        </div>
        <div>
          <dt>Status</dt>
          <dd>
            {isGrant ? (
              <StatusBadge
                tone={(selection.item as { disabled: boolean }).disabled ? "attention" : "ready"}
              >
                {(selection.item as { disabled: boolean }).disabled ? "Disabled" : "Active"}
              </StatusBadge>
            ) : (
              <StatusBadge
                tone={(selection.item as { revoked: boolean }).revoked ? "attention" : "ready"}
              >
                {(selection.item as { revoked: boolean }).revoked ? "Revoked" : "Active"}
              </StatusBadge>
            )}
          </dd>
        </div>
      </dl>

      <h3 className="subsection-title">
        {isGrant ? "Activity in the last 7 days" : "Tool call timeline"}
      </h3>
      {activityQuery.isPending ? (
        <LoadingState
          label={isGrant ? "Loading activity…" : "Loading timeline…"}
          testId="access-activity-loading"
        />
      ) : activityQuery.error ? (
        <ErrorState
          title={isGrant ? "Activity is unavailable." : "Timeline is unavailable."}
          detail="Gateway decision events come from the analytics service, which is not answering on this cluster."
          testId="access-activity-error"
        />
      ) : (
        <AdminTable
          caption="Gateway decisions recorded against this access record."
          columns={columns}
          rows={rows}
          rowKey={(event) =>
            `${event.timestamp || ""}-${eventToolName(event)}-${event.decision || ""}`
          }
          emptyMessage={isGrant ? "No recent activity." : "No tool calls recorded."}
          testId="access-activity-table"
        />
      )}
    </section>
  );
}
