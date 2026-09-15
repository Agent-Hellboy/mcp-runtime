type LoadingStateProps = {
  label: string;
  testId?: string;
};

export function LoadingState({ label, testId = "loading-state" }: LoadingStateProps) {
  return (
    <div className="state-block state-loading" role="status" aria-live="polite" data-testid={testId}>
      <span className="state-spinner" aria-hidden="true" />
      <p className="state-title">{label}</p>
    </div>
  );
}
