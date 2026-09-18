import { useId, type ReactNode } from "react";

import { IconButton } from "./Button";
import { useDismissable, useNarrowViewport } from "./overlay";

type DetailSheetProps = {
  title: string;
  eyebrow?: string;
  onClose: () => void;
  children: ReactNode;
  testId?: string;
  closeLabel?: string;
  closeTestId?: string;
};

// Inspection surface. On a wide screen it docks beside the list and stays
// non-modal, so the list keeps its scroll position and remains operable. Below
// the breakpoint the same content becomes a modal full-screen view, because a
// docked panel has nowhere to dock.
export function DetailSheet({
  title,
  eyebrow,
  onClose,
  children,
  testId = "detail-sheet",
  closeLabel = "Close details",
  closeTestId,
}: DetailSheetProps) {
  const narrow = useNarrowViewport();
  const titleId = useId();
  const ref = useDismissable<HTMLDivElement>({ onDismiss: onClose, trapFocus: narrow });

  const sheet = (
    <div
      ref={ref}
      className={narrow ? "sheet sheet-overlay" : "sheet sheet-docked"}
      role="dialog"
      aria-modal={narrow || undefined}
      aria-labelledby={titleId}
      tabIndex={-1}
      data-testid={testId}
    >
      <div className="sheet-head">
        <div className="sheet-head-text">
          {eyebrow ? <p className="sheet-eyebrow">{eyebrow}</p> : null}
          <h2 className="sheet-title" id={titleId}>
            {title}
          </h2>
        </div>
        <IconButton
          icon="close"
          label={closeLabel}
          bordered
          onClick={onClose}
          data-testid={closeTestId}
          data-autofocus=""
        />
      </div>
      <div className="sheet-body">{children}</div>
    </div>
  );

  if (!narrow) {
    return sheet;
  }

  return (
    <>
      <div className="backdrop" onClick={onClose} aria-hidden="true" />
      {sheet}
    </>
  );
}
