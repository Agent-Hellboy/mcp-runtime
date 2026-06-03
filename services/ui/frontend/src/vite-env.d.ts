/// <reference types="vite/client" />

interface Window {
  MCP_API_BASE?: string;
  MCP_DEFAULTS?: {
    namespace?: string;
    policyVersion?: string;
  };
  MCP_PLATFORM_MODE?: "tenant" | "org" | "public" | string;
  MCP_GOOGLE_CLIENT_ID?: string;
}
