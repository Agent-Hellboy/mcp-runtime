import { useEffect, useRef, useState } from "react";

import { Button } from "../../ui/Button";
import { Icon } from "../../ui/Icon";

type OneTimeKeyNoticeProps = {
  name: string;
  value: string;
  onDismiss: () => void;
};

// The cleartext key exists only in React state for the life of this notice.
// It is never written to localStorage, sessionStorage, a cookie, the URL, or a
// query cache, so a refresh or any navigation loses it permanently - which is
// the intended contract, not a limitation.
export function OneTimeKeyNotice({ name, value, onDismiss }: OneTimeKeyNoticeProps) {
  const headingRef = useRef<HTMLHeadingElement>(null);
  const [copyState, setCopyState] = useState<"idle" | "copied" | "failed">("idle");

  useEffect(() => {
    // Move focus here so a keyboard or screen-reader user lands on the only
    // chance they get to copy this value.
    headingRef.current?.focus();
  }, []);

  async function copy() {
    try {
      if (!navigator.clipboard?.writeText) {
        throw new Error("clipboard unavailable");
      }
      await navigator.clipboard.writeText(value);
      setCopyState("copied");
    } catch {
      setCopyState("failed");
    }
  }

  return (
    <section
      className="one-time-key"
      role="alertdialog"
      aria-labelledby="one-time-key-title"
      aria-describedby="one-time-key-help"
      data-testid="one-time-key"
    >
      <h3 id="one-time-key-title" ref={headingRef} tabIndex={-1} className="section-title">
        <Icon name="key" size={15} /> Copy your new API key now
      </h3>
      <p id="one-time-key-help" className="muted" style={{ marginTop: "var(--space-2)" }}>
        This is the only time <strong>{name}</strong> is shown. Once you dismiss this message the value
        cannot be recovered and you will need to create another key.
      </p>
      <output className="one-time-key-value" data-testid="one-time-key-value">
        {value}
      </output>
      <div className="inline-actions">
        <Button variant="secondary" icon="copy" data-testid="one-time-key-copy" onClick={() => void copy()}>
          Copy to clipboard
        </Button>
        <Button variant="primary" data-testid="one-time-key-dismiss" onClick={onDismiss}>
          I have saved it
        </Button>
        <span role="status" className={copyState === "failed" ? "copy-status fail" : "copy-status ok"}>
          {copyState === "copied"
            ? "Copied to the clipboard."
            : copyState === "failed"
              ? "Copy failed. Select the value above and copy it manually."
              : ""}
        </span>
      </div>
    </section>
  );
}
