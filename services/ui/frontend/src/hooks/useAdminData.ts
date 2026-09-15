import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback } from "react";

import {
  listComponents,
  listEvents,
  listGrants,
  listSessions,
  listTeams,
  readOperations,
} from "../api/admin";
import { UnauthorizedError } from "../api/client";

export const ADMIN_QUERY_KEY = "admin";

export type AdminStatus = "loading" | "ready" | "error" | "unauthorized";

export function adminStatusOf(query: {
  isPending: boolean;
  error: unknown;
}): AdminStatus {
  if (query.error instanceof UnauthorizedError) {
    return "unauthorized";
  }
  if (query.error) {
    return "error";
  }
  return query.isPending ? "loading" : "ready";
}

export function adminErrorMessage(error: unknown): string {
  if (error instanceof Error && error.message.trim()) {
    return error.message.trim();
  }
  return "";
}

// `enabled` is driven by the server-returned principal. A non-admin session
// never issues these requests at all, so the guard fails closed in the network
// layer as well as in the rendered tree.
export function useGrants(enabled: boolean, namespace: string) {
  return useQuery({
    queryKey: [ADMIN_QUERY_KEY, "grants", namespace],
    queryFn: () => listGrants(namespace),
    enabled,
  });
}

export function useSessions(enabled: boolean, namespace: string) {
  return useQuery({
    queryKey: [ADMIN_QUERY_KEY, "sessions", namespace],
    queryFn: () => listSessions(namespace),
    enabled,
  });
}

export function useTeams(enabled: boolean) {
  return useQuery({
    queryKey: [ADMIN_QUERY_KEY, "teams"],
    queryFn: listTeams,
    enabled,
  });
}

export function useComponents(enabled: boolean) {
  return useQuery({
    queryKey: [ADMIN_QUERY_KEY, "components"],
    queryFn: listComponents,
    enabled,
  });
}

export function useOperations(enabled: boolean, user: string) {
  return useQuery({
    queryKey: [ADMIN_QUERY_KEY, "operations", user],
    queryFn: () => readOperations({ user, limit: "100" }),
    enabled,
  });
}

export function useAccessActivity(
  enabled: boolean,
  kind: "grant" | "session",
  name: string
) {
  return useQuery({
    queryKey: [ADMIN_QUERY_KEY, "events", kind, name],
    queryFn: () =>
      listEvents(kind === "session" ? { limit: "1000", session_id: name } : { limit: "1000" }),
    enabled: enabled && Boolean(name),
  });
}

export function useAdminReload() {
  const queryClient = useQueryClient();
  return useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: [ADMIN_QUERY_KEY] });
  }, [queryClient]);
}
