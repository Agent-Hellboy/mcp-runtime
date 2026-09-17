import { StatusBadge, driftTone, riskTone } from "../../ui/Badge";
import { CopyButton } from "../../ui/CopyButton";
import { DetailSheet } from "../../ui/DetailSheet";
import type { ToolRow } from "../../api/types";

type ToolDetailProps = {
  tool: ToolRow;
  onClose: () => void;
};

// Wording verified against the policy model in api/v1alpha1 and the row builder
// in services/runtime-api/internal/runtimeapi/tools.go.
const DRIFT_HELP: Record<string, string> = {
  declared: "The server declares this tool. When a live inventory is available it was also seen live.",
  missing: "The server declares this tool, but the live inventory does not report it.",
  ungoverned: "The server reports this tool live, but nothing declares it, so no governance metadata applies.",
};

function liveNote(tool: ToolRow): string {
  if (tool.live) {
    return "Reported by the running server.";
  }
  if (tool.drift_status === "declared") {
    // tools.go only marks a declared tool "missing" when it actually read a
    // live inventory, so declared + not live means the inventory is unknown.
    return "The live inventory was not available, so liveness is unknown.";
  }
  return "Not reported by the running server.";
}

export function ToolDetail({ tool, onClose }: ToolDetailProps) {
  const labels = Object.entries(tool.labels || {});
  const drift = (tool.drift_status || "").toLowerCase();

  return (
    <DetailSheet
      title={tool.tool_name}
      eyebrow={`${tool.namespace} / ${tool.server_name}`}
      onClose={onClose}
      testId="tool-detail"
      closeLabel="Close tool details"
      closeTestId="tool-detail-close"
    >
      {tool.description ? <p className="muted">{tool.description}</p> : null}

      <dl className="detail-rows">
        <div>
          <dt>Server</dt>
          <dd>
            {tool.server_name}
            <span className="cell-detail">{tool.namespace}</span>
          </dd>
        </div>
        <div>
          <dt>Required trust</dt>
          <dd>
            {tool.required_trust || "—"}
            <span className="cell-detail">
              The minimum trust a caller&apos;s grant or session must carry for the gateway to allow this
              tool.
            </span>
          </dd>
        </div>
        <div>
          <dt>Side effect</dt>
          <dd>
            {tool.side_effect || "—"}
            <span className="cell-detail">Declared by the server as read, write, or destructive.</span>
          </dd>
        </div>
        <div>
          <dt>Risk</dt>
          <dd>
            <StatusBadge tone={riskTone(tool.risk_level)}>{tool.risk_level || "unrated"}</StatusBadge>
            <span className="cell-detail">
              Declared by the server. When it is not declared, the runtime API derives it from the side
              effect and required trust. It is not an independent assessment of the tool.
            </span>
          </dd>
        </div>
        <div>
          <dt>Drift</dt>
          <dd>
            <StatusBadge tone={driftTone(tool.drift_status)}>{tool.drift_status || "unknown"}</StatusBadge>
            <span className="cell-detail">{DRIFT_HELP[drift] || "No drift state was reported."}</span>
          </dd>
        </div>
        <div>
          <dt>Declared</dt>
          <dd>{tool.declared ? "Yes" : "No"}</dd>
        </div>
        <div>
          <dt>Live</dt>
          <dd>
            {tool.live ? "Yes" : "No"}
            <span className="cell-detail">{liveNote(tool)}</span>
          </dd>
        </div>
        {tool.endpoint_url ? (
          <div>
            <dt>Endpoint</dt>
            <dd>
              <span className="copy-row">
                <span className="copy-value" title={tool.endpoint_url}>
                  {tool.endpoint_url}
                </span>
                <CopyButton
                  value={tool.endpoint_url}
                  label={`Copy the endpoint for ${tool.tool_name}`}
                  testId="tool-copy-endpoint"
                />
              </span>
            </dd>
          </div>
        ) : null}
      </dl>

      {labels.length > 0 ? (
        <div>
          <p className="detail-label">Labels</p>
          <ul className="label-list" style={{ marginTop: "var(--space-2)" }}>
            {labels.map(([key, value]) => (
              <li key={key}>
                <span className="label-key">{key}</span>
                <span className="label-value">{value}</span>
              </li>
            ))}
          </ul>
        </div>
      ) : null}
    </DetailSheet>
  );
}
