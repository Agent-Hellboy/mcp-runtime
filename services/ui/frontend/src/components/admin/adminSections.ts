export type AdminSectionId = "access" | "teams" | "operations" | "platform" | "analytics";

export type AdminSection = {
  id: AdminSectionId;
  label: string;
  group: string;
  description: string;
};

// Grouped so the rail reads as governance, then the organisation, then the
// platform itself. Every section that existed before is still here and still
// directly reachable.
export const ADMIN_SECTIONS: AdminSection[] = [
  {
    id: "access",
    label: "Access control",
    group: "Governance",
    description: "Grants and agent sessions enforced by the MCP gateway.",
  },
  {
    id: "teams",
    label: "Teams",
    group: "Organization",
    description: "Tenant teams, their namespaces, and membership.",
  },
  {
    id: "operations",
    label: "Operations",
    group: "Organization",
    description: "Platform users, the audit trail, and image activity.",
  },
  {
    id: "platform",
    label: "Platform health",
    group: "Platform",
    description: "Operator, Sentinel services, and observability components.",
  },
  {
    id: "analytics",
    label: "Usage analytics",
    group: "Platform",
    description: "Gateway events aggregated across the platform.",
  },
];

export function adminSection(id: string): AdminSection {
  return ADMIN_SECTIONS.find((section) => section.id === id) ?? ADMIN_SECTIONS[0];
}

export const ADMIN_GROUPS = ["Governance", "Organization", "Platform"];
