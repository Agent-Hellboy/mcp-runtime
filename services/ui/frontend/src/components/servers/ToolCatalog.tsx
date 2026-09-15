import { StatusBadge, riskTone } from "../StatusBadge";
import { toolKey, type ToolRow } from "../../api/types";

type ToolCatalogProps = {
  tools: ToolRow[];
  totalCount: number;
  selectedToolKey: string;
  onSelectTool: (key: string) => void;
  emptyMessage: string;
};

function driftTone(drift: string): "ready" | "attention" | "neutral" {
  switch (drift) {
    case "declared":
      return "ready";
    case "missing":
    case "ungoverned":
      return "attention";
    default:
      return "neutral";
  }
}

export function ToolCatalog({
  tools,
  totalCount,
  selectedToolKey,
  onSelectTool,
  emptyMessage,
}: ToolCatalogProps) {
  const caption =
    tools.length === totalCount
      ? `${totalCount} tool${totalCount === 1 ? "" : "s"}`
      : `${tools.length} of ${totalCount} tools`;

  return (
    <section className="panel tool-catalog" aria-labelledby="tool-catalog-title">
      <div className="panel-head">
        <h2 id="tool-catalog-title">Tools</h2>
        <p className="panel-count" data-testid="catalog-summary" aria-live="polite">
          {caption}
        </p>
      </div>
      <div className="table-scroll" data-testid="tool-table-scroll" tabIndex={0}>
        <table className="data-table" data-testid="tool-table">
          <caption className="visually-hidden">
            Governed tool catalog with trust, side effect, risk, and drift for each tool.
          </caption>
          <thead>
            <tr>
              <th scope="col">Tool</th>
              <th scope="col">Server</th>
              <th scope="col">Trust</th>
              <th scope="col">Side effect</th>
              <th scope="col">Risk</th>
              <th scope="col">Drift</th>
            </tr>
          </thead>
          <tbody>
            {tools.length === 0 ? (
              <tr>
                <td colSpan={6} className="table-empty" data-testid="tool-table-empty">
                  {emptyMessage}
                </td>
              </tr>
            ) : (
              tools.map((tool) => {
                const key = toolKey(tool);
                const selected = key === selectedToolKey;
                return (
                  <tr
                    key={key}
                    className={selected ? "selected" : undefined}
                    data-testid="tool-row"
                  >
                    <th scope="row">
                      <button
                        type="button"
                        className="link-button"
                        aria-pressed={selected}
                        data-testid="tool-row-select"
                        onClick={() => onSelectTool(selected ? "" : key)}
                      >
                        {tool.tool_name}
                      </button>
                      {tool.description ? (
                        <span className="cell-detail">{tool.description}</span>
                      ) : null}
                    </th>
                    <td>
                      {tool.server_name}
                      <span className="cell-detail">{tool.namespace}</span>
                    </td>
                    <td>{tool.required_trust || "—"}</td>
                    <td>{tool.side_effect || "—"}</td>
                    <td>
                      <StatusBadge tone={riskTone(tool.risk_level)}>
                        {tool.risk_level || "unrated"}
                      </StatusBadge>
                    </td>
                    <td>
                      <StatusBadge tone={driftTone(tool.drift_status)}>
                        {tool.drift_status || "unknown"}
                      </StatusBadge>
                    </td>
                  </tr>
                );
              })
            )}
          </tbody>
        </table>
      </div>
    </section>
  );
}
