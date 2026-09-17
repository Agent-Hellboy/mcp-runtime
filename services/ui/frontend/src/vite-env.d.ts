/// <reference types="vite/client" />

type GoogleIdentityServices = {
  accounts: {
    id: {
      initialize: (config: {
        client_id: string;
        callback: (response: { credential?: string }) => void;
        auto_select?: boolean;
      }) => void;
      renderButton: (
        parent: HTMLElement,
        options: { theme?: string; size?: string; text?: string; width?: number; shape?: string }
      ) => void;
      cancel?: () => void;
    };
  };
};

interface Window {
  MCP_API_BASE?: string;
  MCP_DEFAULTS?: {
    namespace?: string;
    policyVersion?: string;
  };
  MCP_PLATFORM_MODE?: "tenant" | "org" | "public" | string;
  MCP_GOOGLE_CLIENT_ID?: string;
  google?: GoogleIdentityServices;
}
