import { StatusBadge } from "../../ui/Badge";
import { Button } from "../../ui/Button";
import { CopyButton } from "../../ui/CopyButton";
import { DetailSheet } from "../../ui/DetailSheet";
import { formatAbsolute, formatAge } from "../../lib/format";
import { isServerReady, type ServerSummary, type ToolRow } from "../../api/types";

type ServerDetailProps = {
  server: ServerSummary;
  tools: ToolRow[];
  onClose: () => void;
  onShowTools: () => void;
  onSelectTool: (key: string) => void;
};

export function ServerDetail({ server, tools, onClose, onShowTools, onSelectTool }: ServerDetailProps) {
  const ready = isServerReady(server);

  return (
    <DetailSheet
      title={server.name}
      eyebrow={server.namespace}
      onClose={onClose}
      testId="server-detail"
      closeLabel="Close server details"
      closeTestId="server-detail-close"
    >
      <div className="inline-actions">
        <StatusBadge tone={ready ? "ready" : "attention"}>
          {ready ? "Ready" : server.status || "Not ready"}
        </StatusBadge>
        <span className="section-note">
          Kubernetes readiness for the workload. It is not a check of the MCP endpoint.
        </span>
      </div>

      {server.description ? <p className="muted">{server.description}</p> : null}

      <dl className="detail-rows">
        <div>
          <dt>Namespace</dt>
          <dd>{server.namespace}</dd>
        </div>
        <div>
          <dt>Replicas ready</dt>
          <dd className="num">{server.ready || "—"}</dd>
        </div>
        <div>
          <dt>Status</dt>
          <dd>{server.status || "Unknown"}</dd>
        </div>
        {formatAge(server.age) ? (
          <div>
            <dt>Age</dt>
            <dd title={formatAbsolute(server.age)}>{formatAge(server.age)}</dd>
          </div>
        ) : null}
        {server.authMode ? (
          <div>
            <dt>Authentication</dt>
            <dd>{server.authMode}</dd>
          </div>
        ) : null}
        {server.image ? (
          <div>
            <dt>Image</dt>
            <dd>
              <span className="copy-row">
                <span className="copy-value" title={server.image}>
                  {server.image}
                </span>
                <CopyButton value={server.image} label={`Copy the image reference for ${server.name}`} />
              </span>
            </dd>
          </div>
        ) : null}
        {server.endpoint ? (
          <div>
            <dt>Endpoint</dt>
            <dd>
              <span className="copy-row">
                <span className="copy-value" title={server.endpoint}>
                  {server.endpoint}
                </span>
                <CopyButton
                  value={server.endpoint}
                  label={`Copy the endpoint for ${server.name}`}
                  testId="server-detail-copy-endpoint"
                />
              </span>
            </dd>
          </div>
        ) : null}
        {server.uid ? (
          <div>
            <dt>UID</dt>
            <dd className="cell-code">{server.uid}</dd>
          </div>
        ) : null}
      </dl>

      <div>
        <div className="section-head">
          <p className="detail-label">Tools ({tools.length})</p>
          <Button variant="ghost" size="sm" onClick={onShowTools} data-testid="server-detail-show-tools">
            Filter catalog to this server
          </Button>
        </div>
        {tools.length === 0 ? (
          <p className="section-note">This server publishes no tools in the current catalog read.</p>
        ) : (
          <ul className="label-list">
            {tools.slice(0, 40).map((tool) => (
              <li key={`${tool.namespace}/${tool.server_name}/${tool.tool_name}`}>
                <button
                  type="button"
                  className="link-button"
                  onClick={() => onSelectTool(`${tool.namespace}/${tool.server_name}/${tool.tool_name}`)}
                >
                  {tool.tool_name}
                </button>
              </li>
            ))}
          </ul>
        )}
        {tools.length > 40 ? (
          <p className="section-note">Showing the first 40. Use the catalog below for the full list.</p>
        ) : null}
      </div>
    </DetailSheet>
  );
}
