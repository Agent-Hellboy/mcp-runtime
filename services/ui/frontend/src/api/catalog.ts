import { fetchJSON, withQuery } from "./client";
import type { NamespaceEntry, PublishPolicy, ServerSummary, ToolRow } from "./types";

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

export type ServerList = {
  servers: ServerSummary[];
  publishPolicy: PublishPolicy | null;
};

export async function listServers(namespace?: string): Promise<ServerList> {
  const path = withQuery("/runtime/servers", { namespace });
  const data = await fetchJSON(path);
  const record = data && typeof data === "object" ? (data as Record<string, unknown>) : {};
  return {
    servers: asArray<ServerSummary>(data, "servers"),
    publishPolicy: (record.publish_policy as PublishPolicy | undefined) ?? null,
  };
}

export async function listTools(namespace?: string): Promise<ToolRow[]> {
  const path = withQuery("/runtime/tools", { namespace });
  return asArray<ToolRow>(await fetchJSON(path), "tools");
}
