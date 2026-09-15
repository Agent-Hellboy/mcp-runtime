import { useCallback, useEffect, useState } from "react";

import { listNamespaces, listServers, listTools } from "../api/catalog";
import { UnauthorizedError } from "../api/client";
import type { NamespaceEntry, ServerSummary, ToolRow } from "../api/types";

export type CatalogStatus = "loading" | "ready" | "error" | "unauthorized";

export type CatalogState = {
  status: CatalogStatus;
  namespaces: NamespaceEntry[];
  servers: ServerSummary[];
  tools: ToolRow[];
  error: string;
};

const INITIAL: CatalogState = {
  status: "loading",
  namespaces: [],
  servers: [],
  tools: [],
  error: "",
};

function errorMessage(err: unknown): string {
  if (err instanceof Error && err.message.trim()) {
    return err.message.trim();
  }
  return "The catalog could not be loaded.";
}

// useCatalog owns the namespace-scoped catalog read. Namespace changes refetch
// from the server; every other filter is applied client-side by the workspace.
export function useCatalog(enabled: boolean, namespace: string) {
  const [state, setState] = useState<CatalogState>(INITIAL);
  const [reloadToken, setReloadToken] = useState(0);

  const reload = useCallback(() => setReloadToken((token) => token + 1), []);

  useEffect(() => {
    if (!enabled) {
      setState({ ...INITIAL, status: "unauthorized" });
      return;
    }

    let cancelled = false;
    setState((previous) => ({ ...previous, status: "loading", error: "" }));

    (async () => {
      try {
        const [namespaces, servers, tools] = await Promise.all([
          listNamespaces(),
          listServers(namespace),
          listTools(namespace),
        ]);
        if (cancelled) {
          return;
        }
        setState({ status: "ready", namespaces, servers, tools, error: "" });
      } catch (err) {
        if (cancelled) {
          return;
        }
        if (err instanceof UnauthorizedError) {
          setState({ ...INITIAL, status: "unauthorized" });
          return;
        }
        setState({ ...INITIAL, status: "error", error: errorMessage(err) });
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [enabled, namespace, reloadToken]);

  return { ...state, reload };
}
