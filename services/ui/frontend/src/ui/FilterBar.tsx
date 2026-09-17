import type { ReactNode } from "react";

import { Button } from "./Button";
import { IconButton } from "./Button";

type FilterBarProps = {
  label: string;
  /** Filters that are applied on submit rather than as you type. */
  onSubmit?: () => void;
  children: ReactNode;
};

// FilterBar is itself the search form, so callers must never wrap it in
// another <form>: pass onSubmit instead.
export function FilterBar({ label, onSubmit, children }: FilterBarProps) {
  return (
    <form
      className="filter-bar"
      role="search"
      aria-label={label}
      onSubmit={(event) => {
        event.preventDefault();
        onSubmit?.();
      }}
    >
      {children}
    </form>
  );
}

export type FilterChip = {
  id: string;
  label: string;
  value: string;
  onRemove: () => void;
};

type FilterSummaryProps = {
  chips: FilterChip[];
  // Plain-language description of what the count covers.
  count: string;
  onClear?: () => void;
  testId?: string;
};

export function FilterSummary({ chips, count, onClear, testId }: FilterSummaryProps) {
  return (
    <div className="filter-summary" data-testid={testId}>
      <span aria-live="polite">{count}</span>
      {chips.map((chip) => (
        <span className="chip" key={chip.id}>
          <span className="chip-label">{chip.label}:</span>
          <span>{chip.value}</span>
          <IconButton icon="close" label={`Remove ${chip.label} filter`} size="sm" onClick={chip.onRemove} />
        </span>
      ))}
      {chips.length > 0 && onClear ? (
        <Button variant="ghost" size="sm" onClick={onClear} data-testid="reset-filters">
          Clear filters
        </Button>
      ) : null}
    </div>
  );
}
