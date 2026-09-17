import { useEffect, useRef, useState } from "react";

const GSI_SRC = "https://accounts.google.com/gsi/client";

type GoogleSignInButtonProps = {
  clientId: string;
  theme: "dark" | "light";
  onCredential: (idToken: string) => void;
  disabled?: boolean;
};

function loadGSI(): Promise<void> {
  if (window.google?.accounts?.id) {
    return Promise.resolve();
  }
  const existing = document.querySelector<HTMLScriptElement>(`script[src="${GSI_SRC}"]`);
  if (existing) {
    return new Promise((resolve, reject) => {
      existing.addEventListener("load", () => resolve());
      existing.addEventListener("error", () => reject(new Error("gsi_load_failed")));
    });
  }
  return new Promise((resolve, reject) => {
    const script = document.createElement("script");
    script.src = GSI_SRC;
    script.async = true;
    script.defer = true;
    script.onload = () => resolve();
    script.onerror = () => reject(new Error("gsi_load_failed"));
    document.head.appendChild(script);
  });
}

// Google sign-in was previously reachable only from the older dashboard. The
// server still verifies the credential (services/ui/main.go POST /auth/login
// with id_token), so this is the same flow in the redesigned form - the button
// only produces the ID token.
export function GoogleSignInButton({ clientId, theme, onCredential, disabled }: GoogleSignInButtonProps) {
  const container = useRef<HTMLDivElement>(null);
  const [failed, setFailed] = useState(false);
  const callback = useRef(onCredential);
  callback.current = onCredential;

  useEffect(() => {
    let cancelled = false;
    loadGSI()
      .then(() => {
        if (cancelled || !container.current || !window.google?.accounts?.id) {
          return;
        }
        window.google.accounts.id.initialize({
          client_id: clientId,
          callback: (response) => {
            const credential = response?.credential?.trim();
            if (credential) {
              callback.current(credential);
            }
          },
        });
        window.google.accounts.id.renderButton(container.current, {
          theme: theme === "dark" ? "filled_black" : "outline",
          size: "large",
          text: "signin_with",
          shape: "rectangular",
        });
      })
      .catch(() => {
        if (!cancelled) {
          setFailed(true);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [clientId, theme]);

  if (failed) {
    return (
      <p className="field-error" data-testid="google-signin-error">
        Google sign-in could not be loaded. Use your email and password or an API key.
      </p>
    );
  }

  return (
    <div
      className="google-signin"
      ref={container}
      data-testid="google-signin"
      aria-busy={disabled || undefined}
    />
  );
}
