import { StatusBadge, riskTone } from "../StatusBadge";
import type { ToolRow } from "../../api/types";

type ToolDetailProps = {
  tool: ToolRow;
  onClose: () => void;
};

export function ToolDetail({ tool, onClose }: ToolDetailProps) {
  const labels = Object.entries(tool.labels || {});
  return (
    <aside className="panel tool-detail" aria-labelledby="tool-detail-title" data-testid="tool-detail">
      <div className="panel-head">
        <h2 id="tool-detail-title">{tool.tool_name}</h2>
        <button type="button" className="button ghost" onClick={onClose} data-testid="tool-detail-close">
          Close
        </button>
      </div>
      {tool.description ? <p className="panel-lede">{tool.description}</p> : null}
      <dl className="detail-grid">
        <div>
          <dt>Server</dt>
          <dd>{tool.server_name}</dd>
        </div>
        <div>
          <dt>Namespace</dt>
          <dd>{tool.namespace}</dd>
        </div>
        <div>
          <dt>Required trust</dt>
          <dd>{tool.required_trust || "—"}</dd>
        </div>
        <div>
          <dt>Side effect</dt>
          <dd>{tool.side_effect || "—"}</dd>
        </div>
        <div>
          <dt>Risk</dt>
          <dd>
            <StatusBadge tone={riskTone(tool.risk_level)}>
              {tool.risk_level || "unrated"}
            </StatusBadge>
          </dd>
        </div>
        <div>
          <dt>Drift</dt>
          <dd>{tool.drift_status}</dd>
        </div>
        <div>
          <dt>Declared</dt>
          <dd>{tool.declared ? "yes" : "no"}</dd>
        </div>
        <div>
          <dt>Live</dt>
          <dd>{tool.live ? "yes" : "no"}</dd>
        </div>
      </dl>
      {tool.endpoint_url ? (
        <p className="tool-detail-endpoint">
          <span className="detail-label">Endpoint</span>
          <code>{tool.endpoint_url}</code>
        </p>
      ) : null}
      {labels.length > 0 ? (
        <ul className="label-list">
          {labels.map(([key, value]) => (
            <li key={key}>
              <span className="label-key">{key}</span>
              <span className="label-value">{value}</span>
            </li>
          ))}
        </ul>
      ) : null}
    </aside>
  );
}
