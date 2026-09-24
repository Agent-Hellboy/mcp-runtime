import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { StatusBadge, riskTone } from "../../ui/Badge";
import { Button } from "../../ui/Button";
import { CopyButton } from "../../ui/CopyButton";
import { DetailSheet } from "../../ui/DetailSheet";
import { formatAbsolute, formatAge } from "../../lib/format";
import {
  authModeInfo,
  isServerReady,
  serverPrompts,
  serverResources,
  serverTasks,
  toolKey,
  type ServerSummary,
  type ToolRow,
} from "../../api/types";
import { listServerEvents } from "../../api/catalog";

type ServerDetailProps = {
  server: ServerSummary;
  tools: ToolRow[];
  onClose: () => void;
  onShowTools: () => void;
  onSelectTool: (key: string) => void;
};

export function ServerDetail({ server, tools, onClose, onShowTools, onSelectTool }: ServerDetailProps) {
  const [configTab, setConfigTab] = useState<"claude" | "cursor" | "vscode" | "raw">("claude");
  const ready = isServerReady(server);
  const auth = authModeInfo(server.authMode);
  const prompts = serverPrompts(server);
  const resources = serverResources(server);
  const tasks = serverTasks(server);

  // The runtime hands back a complete MCP client config for this server. It is
  // shown in full and selectable, so it is usable even if the clipboard is
  // blocked.
  const connectConfig = useMemo(
    () =>
      server.access_json && Object.keys(server.access_json).length
        ? JSON.stringify(server.access_json, null, 2)
        : "",
    [server.access_json]
  );

  // Prefer the catalog rows (they carry drift and the resolved risk); fall back
  // to the declared tools on the server record when the catalog read is scoped
  // elsewhere.
  const toolRows = useMemo(() => {
    if (tools.length > 0) {
      return tools.map((tool) => ({
        key: toolKey(tool),
        name: tool.tool_name,
        description: tool.description,
        trust: tool.required_trust,
        sideEffect: tool.side_effect,
        risk: tool.risk_level,
        selectable: true,
      }));
    }
    return (server.tools || []).map((tool) => ({
      key: `${server.namespace}/${server.name}/${tool.name || ""}`,
      name: tool.name || "—",
      description: tool.description,
      trust: tool.requiredTrust,
      sideEffect: tool.sideEffect,
      risk: tool.riskLevel,
      selectable: false,
    }));
  }, [tools, server]);

  const eventsQuery = useQuery({
    queryKey: ["server-events", server.namespace, server.name],
    queryFn: () => listServerEvents(server.namespace, server.name),
    staleTime: 30_000,
  });

  const configTabs = useMemo(() => {
    const name = server.name || "mcp-server";
    const url = server.endpoint || "";
    return {
      claude: { label: "Claude Desktop", hint: "~/Library/Application Support/Claude/claude_desktop_config.json", value: JSON.stringify({ mcpServers: { [name]: { type: "http", url } } }, null, 2) },
      cursor: { label: "Cursor", hint: "~/.cursor/mcp.json or .cursor/mcp.json", value: JSON.stringify({ mcpServers: { [name]: { type: "http", url } } }, null, 2) },
      vscode: { label: "VS Code", hint: ".vscode/mcp.json", value: JSON.stringify({ servers: { [name]: { type: "http", url } } }, null, 2) },
      raw: { label: "Raw JSON", hint: "The server-provided access configuration", value: JSON.stringify(server.access_json || {}, null, 2) },
    };
  }, [server]);

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
        <StatusBadge tone={auth.tone}>{auth.label}</StatusBadge>
      </div>

      {server.description ? <p className="muted">{server.description}</p> : null}

      <div>
        <p className="detail-label">Authentication</p>
        <p className="section-note" data-testid="server-detail-auth">
          {auth.detail}
        </p>
      </div>

      {connectConfig ? (
        <div>
          <div className="section-head">
            <p className="detail-label">Connect from an MCP client</p>
            <CopyButton
              value={connectConfig}
              label={`Copy the MCP client config for ${server.name}`}
              testId="server-detail-copy-config"
            />
          </div>
          <div className="detail-tabs" role="tablist" aria-label="MCP client configuration">
            {(Object.keys(configTabs) as Array<keyof typeof configTabs>).map((key) => (
              <button key={key} type="button" role="tab" aria-selected={configTab === key} className={configTab === key ? "detail-tab is-active" : "detail-tab"} onClick={() => setConfigTab(key)}>
                {configTabs[key].label}
              </button>
            ))}
          </div>
          <p className="section-note" style={{ marginBottom: "var(--space-2)" }}>
            Paste this into {configTabs[configTab].hint}.
            {auth.label === "OAuth"
              ? ` The client must also satisfy ${auth.label} before calls are allowed.`
              : ""}
          </p>
          <code className="code-block" data-testid="server-detail-config">
            {configTabs[configTab].value}
          </code>
        </div>
      ) : (
        <p className="section-note" data-testid="server-detail-no-config">
          The runtime did not resolve a public connect endpoint for this server, so there is no client
          config to copy.
        </p>
      )}

      <dl className="detail-rows">
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
        {prompts.length || resources.length || tasks.length ? (
          <div>
            <dt>Also serves</dt>
            <dd>
              {[
                prompts.length ? `${prompts.length} prompts` : "",
                resources.length ? `${resources.length} resources` : "",
                tasks.length ? `${tasks.length} tasks` : "",
              ]
                .filter(Boolean)
                .join(" · ")}
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
          <p className="detail-label">Tools ({toolRows.length})</p>
          <Button variant="ghost" size="sm" onClick={onShowTools} data-testid="server-detail-show-tools">
            Filter catalog to this server
          </Button>
        </div>
        {toolRows.length === 0 ? (
          <p className="section-note">This server publishes no tools in the current catalog read.</p>
        ) : (
          <div className="tool-scroll-list" data-testid="server-detail-tools" tabIndex={0}>
            {toolRows.map((tool) => {
              const body = (
                <>
                  <span className="tool-scroll-name">
                    {tool.name}
                    {tool.risk ? (
                      <StatusBadge tone={riskTone(tool.risk)} dot={false}>
                        {tool.risk}
                      </StatusBadge>
                    ) : null}
                    {tool.trust ? (
                      <StatusBadge tone="neutral" dot={false}>
                        {`trust: ${tool.trust}`}
                      </StatusBadge>
                    ) : null}
                    {tool.sideEffect ? (
                      <StatusBadge tone="neutral" dot={false}>
                        {tool.sideEffect}
                      </StatusBadge>
                    ) : null}
                  </span>
                  {tool.description ? <span className="tool-scroll-desc">{tool.description}</span> : null}
                </>
              );
              return tool.selectable ? (
                <button
                  key={tool.key}
                  type="button"
                  className="tool-scroll-item"
                  data-testid="server-detail-tool"
                  onClick={() => onSelectTool(tool.key)}
                >
                  {body}
                </button>
              ) : (
                <div key={tool.key} className="tool-scroll-item" data-testid="server-detail-tool">
                  {body}
                </div>
              );
            })}
          </div>
        )}
      </div>

      <div>
        <div className="section-head">
          <p className="detail-label">Recent activity</p>
          <span className="section-note">Last 20 events</span>
        </div>
        {eventsQuery.isPending ? <p className="section-note">Loading activity…</p> : null}
        {eventsQuery.error ? <p className="section-note">Activity is unavailable for this server.</p> : null}
        {!eventsQuery.isPending && !eventsQuery.error && (eventsQuery.data || []).length === 0 ? <p className="section-note">No recent activity recorded.</p> : null}
        {(eventsQuery.data || []).length > 0 ? (
          <div className="event-list" data-testid="server-detail-events">
            {(eventsQuery.data || []).map((event, index) => (
              <div className="event-row" key={`${event.timestamp || "event"}-${index}`}>
                <span>{event.tool_name || event.event_type || "Gateway event"}</span>
                <StatusBadge tone={event.decision === "deny" ? "warning" : "ready"}>{event.decision || "unknown"}</StatusBadge>
                <time dateTime={event.timestamp}>{event.timestamp ? new Date(event.timestamp).toLocaleString() : "—"}</time>
              </div>
            ))}
          </div>
        ) : null}
      </div>
    </DetailSheet>
  );
}
