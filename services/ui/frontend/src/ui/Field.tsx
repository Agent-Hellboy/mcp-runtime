import { useId, type InputHTMLAttributes, type ReactNode, type SelectHTMLAttributes } from "react";

import { Icon } from "./Icon";

type FieldShellProps = {
  label: string;
  hint?: string;
  error?: string;
  required?: boolean;
  className?: string;
  errorTestId?: string;
  /** Announce the error as it appears, for submit-time validation. */
  announceError?: boolean;
  children: (ids: { inputId: string; describedBy: string | undefined; invalid: boolean }) => ReactNode;
};

// Field owns the label/hint/error wiring so no screen has to remember
// aria-describedby and aria-invalid by hand.
export function Field({
  label,
  hint,
  error,
  required,
  className,
  errorTestId,
  announceError,
  children,
}: FieldShellProps) {
  const inputId = useId();
  const hintId = `${inputId}-hint`;
  const errorId = `${inputId}-error`;
  const describedBy = [hint ? hintId : "", error ? errorId : ""].filter(Boolean).join(" ") || undefined;

  return (
    <div className={className ? `field ${className}` : "field"}>
      <label className="field-label" htmlFor={inputId}>
        {label}
        {required ? <span className="field-required"> (required)</span> : null}
      </label>
      {children({ inputId, describedBy, invalid: Boolean(error) })}
      {hint ? (
        <span className="field-hint" id={hintId}>
          {hint}
        </span>
      ) : null}
      {error ? (
        <span
          className="field-error"
          id={errorId}
          role={announceError ? "alert" : undefined}
          data-testid={errorTestId}
        >
          <Icon name="alert" size={13} />
          {error}
        </span>
      ) : null}
    </div>
  );
}

type TextFieldProps = Omit<InputHTMLAttributes<HTMLInputElement>, "id"> & {
  label: string;
  hint?: string;
  error?: string;
  fieldClassName?: string;
  leadingIcon?: boolean;
  errorTestId?: string;
  announceError?: boolean;
};

export function TextField({
  label,
  hint,
  error,
  fieldClassName,
  leadingIcon,
  errorTestId,
  announceError,
  required,
  className,
  ...rest
}: TextFieldProps) {
  return (
    <Field
      label={label}
      hint={hint}
      error={error}
      required={required}
      className={fieldClassName}
      errorTestId={errorTestId}
      announceError={announceError}
    >
      {({ inputId, describedBy, invalid }) => {
        const input = (
          <input
            id={inputId}
            className={className ? `input ${className}` : "input"}
            aria-describedby={describedBy}
            aria-invalid={invalid || undefined}
            required={required}
            {...rest}
          />
        );
        if (!leadingIcon) {
          return input;
        }
        return (
          <span className="input-with-icon">
            <Icon name="search" size={15} />
            {input}
          </span>
        );
      }}
    </Field>
  );
}

type SelectFieldProps = Omit<SelectHTMLAttributes<HTMLSelectElement>, "id"> & {
  label: string;
  hint?: string;
  error?: string;
  fieldClassName?: string;
  errorTestId?: string;
  announceError?: boolean;
  options: Array<{ value: string; label: string; disabled?: boolean }>;
};

export function SelectField({
  label,
  hint,
  error,
  fieldClassName,
  errorTestId,
  announceError,
  options,
  required,
  className,
  ...rest
}: SelectFieldProps) {
  return (
    <Field
      label={label}
      hint={hint}
      error={error}
      required={required}
      className={fieldClassName}
      errorTestId={errorTestId}
      announceError={announceError}
    >
      {({ inputId, describedBy, invalid }) => (
        <select
          id={inputId}
          className={className ? `select ${className}` : "select"}
          aria-describedby={describedBy}
          aria-invalid={invalid || undefined}
          required={required}
          {...rest}
        >
          {options.map((option) => (
            <option key={option.value} value={option.value} disabled={option.disabled}>
              {option.label}
            </option>
          ))}
        </select>
      )}
    </Field>
  );
}
