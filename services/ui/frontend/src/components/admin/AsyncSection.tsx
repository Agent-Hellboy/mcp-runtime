import type { ReactNode } from "react";

import { ErrorState } from "../ErrorState";
import { LoadingState } from "../LoadingState";
import { adminErrorMessage, adminStatusOf } from "../../hooks/useAdminData";

type AsyncSectionProps = {
  query: { isPending: boolean; error: unknown };
  loadingLabel: string;
  errorTitle: string;
  onRetry?: () => void;
  onSignIn?: () => void;
  testId: string;
  children: ReactNode;
};

// Renders the loading / session-expired / error states for one admin query and
// only yields to `children` once the read succeeded.
export function AsyncSection({
  query,
  loadingLabel,
  errorTitle,
  onRetry,
  onSignIn,
  testId,
  children,
}: AsyncSectionProps) {
  const status = adminStatusOf(query);

  if (status === "loading") {
    return <LoadingState label={loadingLabel} testId={`${testId}-loading`} />;
  }

  if (status === "unauthorized") {
    return (
      <ErrorState
        title="Your session expired."
        detail="Sign in again to reload this view."
        onRetry={onSignIn}
        retryLabel="Sign in"
        testId={`${testId}-unauthorized`}
      />
    );
  }

  if (status === "error") {
    return (
      <ErrorState
        title={errorTitle}
        detail={adminErrorMessage(query.error)}
        onRetry={onRetry}
        testId={`${testId}-error`}
      />
    );
  }

  return <>{children}</>;
}
