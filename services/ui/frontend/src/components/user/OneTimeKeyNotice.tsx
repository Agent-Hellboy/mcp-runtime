import { useEffect, useRef } from "react";

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

  useEffect(() => {
    // Move focus here so a keyboard or screen-reader user lands on the only
    // chance they get to copy this value.
    headingRef.current?.focus();
  }, []);

  return (
    <section
      className="panel one-time-key"
      role="alertdialog"
      aria-labelledby="one-time-key-title"
      aria-describedby="one-time-key-help"
      data-testid="one-time-key"
    >
      <h3 id="one-time-key-title" ref={headingRef} tabIndex={-1}>
        Copy your new API key now
      </h3>
      <p id="one-time-key-help" className="panel-lede">
        This is the only time <strong>{name}</strong> is shown. Once you dismiss this message the
        value cannot be recovered and you will need to create another key.
      </p>
      <output className="one-time-key-value" data-testid="one-time-key-value">
        {value}
      </output>
      <div className="form-actions">
        <button
          type="button"
          className="button ghost"
          data-testid="one-time-key-copy"
          onClick={() => {
            // Clipboard access can be denied or unavailable; the value stays
            // selectable on screen either way, so a failure is not fatal.
            void navigator.clipboard?.writeText(value).catch(() => {});
          }}
        >
          Copy to clipboard
        </button>
        <button
          type="button"
          className="button primary"
          data-testid="one-time-key-dismiss"
          onClick={onDismiss}
        >
          I have saved it
        </button>
      </div>
    </section>
  );
}
