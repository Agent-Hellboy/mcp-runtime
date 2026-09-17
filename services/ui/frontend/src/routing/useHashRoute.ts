import { useCallback, useEffect, useState } from "react";

import { HOME, formatRoute, parseRoute, sameRoute, type Route, type WorkspaceId } from "./route";

export type Navigate = (
  next: Partial<Route> & { workspace: WorkspaceId },
  options?: { replace?: boolean }
) => void;

function currentRoute(): Route {
  return parseRoute(typeof window === "undefined" ? "" : window.location.hash);
}

export function useHashRoute(): { route: Route; navigate: Navigate; setParams: (params: Record<string, string | undefined>) => void } {
  const [route, setRoute] = useState<Route>(currentRoute);

  useEffect(() => {
    function sync() {
      setRoute(currentRoute());
    }
    window.addEventListener("hashchange", sync);
    // A hash written before React mounted (a shared deep link) is picked up here.
    sync();
    return () => window.removeEventListener("hashchange", sync);
  }, []);

  const navigate = useCallback<Navigate>((next, options) => {
    const target: Route = {
      workspace: next.workspace,
      section: next.section ?? "",
      params: next.params ?? {},
    };
    const hash = formatRoute(target);
    if (options?.replace) {
      window.history.replaceState(null, "", hash);
      setRoute(target);
      return;
    }
    if (hash === window.location.hash) {
      setRoute(target);
      return;
    }
    window.location.hash = hash;
    setRoute(target);
  }, []);

  // Updates one or more query values on the current route without touching the
  // workspace, so a selection is shareable but does not create a new history
  // entry for every keystroke-sized change.
  const setParams = useCallback((params: Record<string, string | undefined>) => {
    setRoute((current) => {
      const nextParams = { ...current.params };
      for (const [key, value] of Object.entries(params)) {
        if (value) {
          nextParams[key] = value;
        } else {
          delete nextParams[key];
        }
      }
      const next = { ...current, params: nextParams };
      if (sameRoute(current, next)) {
        return current;
      }
      window.history.replaceState(null, "", formatRoute(next));
      return next;
    });
  }, []);

  return { route: route ?? HOME, navigate, setParams };
}
