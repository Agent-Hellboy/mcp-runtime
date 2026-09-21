import { useEffect, useRef } from "react";

import { readRuntimeConfig } from "../api/config";

const GSI_SCRIPT_SRC = "https://accounts.google.com/gsi/client";

let gsiScriptPromise: Promise<void> | null = null;

// Loads the Google Identity Services script at most once per page load,
// regardless of how many times a SignInPanel mounts.
function loadGoogleIdentityServices(): Promise<void> {
  if (window.google?.accounts?.id) {
    return Promise.resolve();
  }
  if (!gsiScriptPromise) {
    gsiScriptPromise = new Promise((resolve, reject) => {
      const script = document.createElement("script");
      script.src = GSI_SCRIPT_SRC;
      script.async = true;
      script.defer = true;
      script.onload = () => resolve();
      script.onerror = () => reject(new Error("failed to load Google Identity Services"));
      document.head.appendChild(script);
    });
  }
  return gsiScriptPromise;
}

type GoogleSignInButtonProps = {
  onCredential: (idToken: string) => void;
  onError?: (message: string) => void;
};

// Renders nothing when no client ID is configured for this deployment
// (readRuntimeConfig().googleClientId, from window.MCP_GOOGLE_CLIENT_ID).
export function GoogleSignInButton({ onCredential, onError }: GoogleSignInButtonProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const clientId = readRuntimeConfig().googleClientId;

  useEffect(() => {
    if (!clientId) {
      return;
    }
    let cancelled = false;

    loadGoogleIdentityServices()
      .then(() => {
        if (cancelled || !containerRef.current || !window.google?.accounts?.id) {
          return;
        }
        window.google.accounts.id.initialize({
          client_id: clientId,
          callback: (response) => {
            if (response.credential) {
              onCredential(response.credential);
            } else {
              onError?.("Google sign-in did not return a credential. Try again.");
            }
          },
        });
        window.google.accounts.id.renderButton(containerRef.current, {
          theme: "outline",
          size: "large",
          shape: "pill",
          text: "continue_with",
          width: 280,
        });
      })
      .catch(() => {
        onError?.("Google sign-in is unavailable right now. Use another sign-in method or try again.");
      });

    return () => {
      cancelled = true;
    };
  }, [clientId, onCredential, onError]);

  if (!clientId) {
    return null;
  }

  return <div ref={containerRef} className="google-signin" data-testid="google-signin" />;
}
