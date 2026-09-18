import { useMemo, useState } from "react";

import { ServerList } from "./ServerList";
import { ToolCatalog } from "./ToolCatalog";
import { ToolDetail } from "./ToolDetail";
import { EMPTY_TOOL_FILTERS, countToolsByServer, type ToolFilters } from "./filters";
import { Button } from "../../ui/Button";
import { PageHeader } from "../../ui/PageHeader";
import { ErrorState, LoadingState } from "../../ui/States";
import { usePublicCatalog } from "../../hooks/useCatalog";
import { serverKey, toolKey } from "../../api/types";

// The anonymous counterpart of ServersWorkspace, for a signed-out visitor to a
// PLATFORM_MODE=public deployment. Read-only: no namespace or status filtering,
// no retire, and no publish quota - just the public catalog the backend
// resolves for an unauthenticated caller.
export function PublicServersPreview({ onSignIn }: { onSignIn: () => void }) {
  const [toolFilters, setToolFilters] = useState<ToolFilters>(EMPTY_TOOL_FILTERS);
  const [selectedToolKey, setSelectedToolKey] = useState("");
  const catalog = usePublicCatalog(true);

  const toolCounts = useMemo(() => countToolsByServer(catalog.tools), [catalog.tools]);

  const visibleTools = useMemo(() => {
    const term = toolFilters.search.trim().toLowerCase();
    return catalog.tools.filter((tool) => {
      if (toolFilters.serverKey && `${tool.namespace}/${tool.server_name}` !== toolFilters.serverKey) {
        return false;
      }
      if (toolFilters.risk && (tool.risk_level || "").toLowerCase() !== toolFilters.risk) {
        return false;
      }
      if (toolFilters.drift && (tool.drift_status || "").toLowerCase() !== toolFilters.drift) {
        return false;
      }
      if (!term) {
        return true;
      }
      return [tool.tool_name, tool.description, tool.server_name]
        .filter(Boolean)
        .join(" ")
        .toLowerCase()
        .includes(term);
    });
  }, [catalog.tools, toolFilters]);

  const selectedTool = useMemo(
    () => visibleTools.find((tool) => toolKey(tool) === selectedToolKey),
    [visibleTools, selectedToolKey]
  );

  const header = (
    <PageHeader
      title="Public catalog"
      description="Browsing anonymously. Sign in to see your organization's full catalog and manage servers."
      actions={
        <Button variant="primary" icon="login" onClick={onSignIn} data-testid="public-catalog-sign-in">
          Sign in
        </Button>
      }
    />
  );

  if (catalog.status === "loading") {
    return (
      <>
        {header}
        <LoadingState label="Loading the public catalog…" testId="public-catalog-loading" variant="cards" rows={3} />
      </>
    );
  }

  if (catalog.status === "error") {
    return (
      <>
        {header}
        <ErrorState
          title="The public catalog could not be loaded."
          detail={catalog.error}
          testId="public-catalog-error"
        />
      </>
    );
  }

  const scopedServerName = toolFilters.serverKey
    ? catalog.servers.find((server) => serverKey(server) === toolFilters.serverKey)?.name || ""
    : "";

  return (
    <>
      {header}

      <section className="section">
        <div className="section-head">
          <h2 className="section-title">Servers</h2>
          <p className="section-note">{catalog.servers.length} published publicly</p>
        </div>
        <ServerList
          servers={catalog.servers}
          toolCounts={toolCounts}
          scopedServerKey={toolFilters.serverKey}
          inspectedServerKey=""
          filtered={false}
          onClearFilters={() => setToolFilters(EMPTY_TOOL_FILTERS)}
          onScope={(key) => {
            setToolFilters((current) => ({ ...current, serverKey: key }));
            setSelectedToolKey("");
          }}
          onInspect={() => {}}
        />
      </section>

      <ToolCatalog
        tools={visibleTools}
        scopedCount={catalog.tools.length}
        filters={toolFilters}
        scopedServerName={scopedServerName}
        selectedToolKey={selectedToolKey}
        onFiltersChange={setToolFilters}
        onClearFilters={() => {
          setToolFilters(EMPTY_TOOL_FILTERS);
          setSelectedToolKey("");
        }}
        onSelectTool={setSelectedToolKey}
      />

      {selectedTool ? (
        <ToolDetail tool={selectedTool} onClose={() => setSelectedToolKey("")} />
      ) : null}
    </>
  );
}
