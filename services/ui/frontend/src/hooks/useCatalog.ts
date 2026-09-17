import { useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback } from "react";

import { listNamespaces, listPublicServers, listPublicTools, listServers, listTools } from "../api/catalog";
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
    // True while a refetch of already-loaded data is in flight, so Refresh can
    // show real progress instead of pretending the click did nothing.
    refreshing: status === "ready" && results.some((result) => result.isFetching),
    namespaces: (namespacesQuery.data ?? []) as NamespaceEntry[],
    servers: serverList?.servers ?? [],
    publishPolicy: serverList?.publishPolicy ?? null,
    tools: (toolsQuery.data ?? []) as ToolRow[],
    error: firstError ? errorMessage(firstError) : "",
    reload,
  };
}

export type PublicCatalogStatus = "loading" | "ready" | "error";

// The anonymous counterpart to useCatalog, for a signed-out visitor to a
// PLATFORM_MODE=public deployment (services/ui/public_catalog_proxy.go).
// There is no namespace scoping and no namespaces list - the backend
// resolves a single implicit public namespace, matching the removed legacy
// dashboard's synthesized "public preview" scope.
export function usePublicCatalog(enabled: boolean) {
  const serversQuery = useQuery({
    queryKey: [CATALOG_QUERY_KEY, "public", "servers"],
    queryFn: listPublicServers,
    enabled,
  });
  const toolsQuery = useQuery({
    queryKey: [CATALOG_QUERY_KEY, "public", "tools"],
    queryFn: listPublicTools,
    enabled,
  });

  const firstError = serversQuery.error || toolsQuery.error;
  let status: PublicCatalogStatus;
  if (!enabled) {
    status = "loading";
  } else if (firstError) {
    status = "error";
  } else if (serversQuery.isPending || toolsQuery.isPending) {
    status = "loading";
  } else {
    status = "ready";
  }

  return {
    status,
    servers: serversQuery.data ?? [],
    tools: toolsQuery.data ?? [],
    error: firstError ? errorMessage(firstError) : "",
  };
}
