export type RuntimeConfig = {
  apiBase: string;
  defaults: {
    namespace: string;
    policyVersion: string;
  };
  platformMode: string;
  googleClientId: string;
};

export function readRuntimeConfig(): RuntimeConfig {
  return {
    apiBase: window.MCP_API_BASE || "/api",
    defaults: {
      namespace: window.MCP_DEFAULTS?.namespace || "",
      policyVersion: window.MCP_DEFAULTS?.policyVersion || "v1",
    },
    platformMode: window.MCP_PLATFORM_MODE || "tenant",
    googleClientId: window.MCP_GOOGLE_CLIENT_ID || "",
  };
}
