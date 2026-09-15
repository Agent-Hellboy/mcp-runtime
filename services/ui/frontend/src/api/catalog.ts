import { fetchJSON, withQuery } from "./client";
import type { NamespaceEntry, ServerSummary, ToolRow } from "./types";

function asArray<T>(value: unknown, key: string): T[] {
  if (!value || typeof value !== "object") {
    return [];
  }
  const items = (value as Record<string, unknown>)[key];
  return Array.isArray(items) ? (items as T[]) : [];
}

export async function listNamespaces(): Promise<NamespaceEntry[]> {
  return asArray<NamespaceEntry>(await fetchJSON("/runtime/namespaces"), "namespaces");
}

export async function listServers(namespace?: string): Promise<ServerSummary[]> {
  const path = withQuery("/runtime/servers", { namespace });
  return asArray<ServerSummary>(await fetchJSON(path), "servers");
}

export async function listTools(namespace?: string): Promise<ToolRow[]> {
  const path = withQuery("/runtime/tools", { namespace });
  return asArray<ToolRow>(await fetchJSON(path), "tools");
}
