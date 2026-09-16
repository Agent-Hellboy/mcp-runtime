import { useQueries, useQueryClient } from "@tanstack/react-query";
import { useCallback } from "react";

import { listNamespaces, listServers, listTools } from "../api/catalog";
import { UnauthorizedError } from "../api/client";
import type { NamespaceEntry, PublishPolicy, ServerSummary, ToolRow } from "../api/types";

export type CatalogStatus = "loading" | "ready" | "error" | "unauthorized";

export const CATALOG_QUERY_KEY = "catalog";

function errorMessage(err: unknown): string {
  if (err instanceof Error && err.message.trim()) {
    return err.message.trim();
  }
  return "The catalog could not be loaded.";
}

// useCatalog owns the namespace-scoped catalog read. The namespace is part of
// the query key, so changing scope refetches and caches per scope; every other
// filter is applied client-side by the workspace.
export function useCatalog(enabled: boolean, namespace: string) {
  const queryClient = useQueryClient();

  const results = useQueries({
    queries: [
      {
        queryKey: [CATALOG_QUERY_KEY, "namespaces"],
        queryFn: listNamespaces,
        enabled,
      },
      {
        queryKey: [CATALOG_QUERY_KEY, "servers", namespace],
        queryFn: () => listServers(namespace),
        enabled,
      },
      {
        queryKey: [CATALOG_QUERY_KEY, "tools", namespace],
        queryFn: () => listTools(namespace),
        enabled,
      },
    ],
  });

  const [namespacesQuery, serversQuery, toolsQuery] = results;
  const firstError = results.find((result) => result.error)?.error;

  const reload = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: [CATALOG_QUERY_KEY] });
  }, [queryClient]);

  let status: CatalogStatus;
  if (!enabled) {
    status = "unauthorized";
  } else if (firstError instanceof UnauthorizedError) {
    status = "unauthorized";
  } else if (firstError) {
    status = "error";
  } else if (results.some((result) => result.isPending)) {
    status = "loading";
  } else {
    status = "ready";
  }

  const serverList = serversQuery.data as { servers: ServerSummary[]; publishPolicy: PublishPolicy | null } | undefined;

  return {
    status,
    namespaces: (namespacesQuery.data ?? []) as NamespaceEntry[],
    servers: serverList?.servers ?? [],
    publishPolicy: serverList?.publishPolicy ?? null,
    tools: (toolsQuery.data ?? []) as ToolRow[],
    error: firstError ? errorMessage(firstError) : "",
    reload,
  };
}
