import { useEffect, useRef, useState } from "react";

const FOCUSABLE =
  'a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])';

// True below the layout breakpoint where docked side sheets become full-screen
// overlays. jsdom has no matchMedia by default, so the hook degrades to "wide".
export function useNarrowViewport(query = "(max-width: 900px)"): boolean {
  const [narrow, setNarrow] = useState(false);

  useEffect(() => {
    if (typeof window.matchMedia !== "function") {
      return;
    }
    const media = window.matchMedia(query);
    const update = () => setNarrow(media.matches);
    update();
    media.addEventListener?.("change", update);
    return () => media.removeEventListener?.("change", update);
  }, [query]);

  return narrow;
}

type DismissableOptions = {
  onDismiss: () => void;
  // Modal overlays trap Tab and block the page behind them; docked inspectors
  // stay in the normal tab order.
  trapFocus: boolean;
  active?: boolean;
};

// Shared overlay behaviour: focus moves in on open, Escape dismisses, focus
// returns to whatever opened it, and modal overlays keep Tab inside.
export function useDismissable<T extends HTMLElement>({
  onDismiss,
  trapFocus,
  active = true,
}: DismissableOptions) {
  const ref = useRef<T>(null);
  const opener = useRef<Element | null>(null);

  useEffect(() => {
    if (!active) {
      return;
    }
    opener.current = document.activeElement;
    const node = ref.current;
    const target = node?.querySelector<HTMLElement>("[data-autofocus]") || node;
    target?.focus();

    return () => {
      const previous = opener.current;
      if (previous instanceof HTMLElement && document.contains(previous)) {
        previous.focus();
      }
    };
  }, [active]);

  useEffect(() => {
    if (!active) {
      return;
    }
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") {
        event.stopPropagation();
        onDismiss();
        return;
      }
      if (event.key !== "Tab" || !trapFocus) {
        return;
      }
      const node = ref.current;
      if (!node) {
        return;
      }
      const items = Array.from(node.querySelectorAll<HTMLElement>(FOCUSABLE)).filter(
        (item) => item.offsetParent !== null || item === document.activeElement
      );
      if (items.length === 0) {
        event.preventDefault();
        node.focus();
        return;
      }
      const first = items[0];
      const last = items[items.length - 1];
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    }

    document.addEventListener("keydown", onKeyDown, true);
    return () => document.removeEventListener("keydown", onKeyDown, true);
  }, [active, onDismiss, trapFocus]);

  return ref;
}
