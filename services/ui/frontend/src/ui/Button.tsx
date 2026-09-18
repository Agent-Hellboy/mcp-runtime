import type { AnchorHTMLAttributes, ButtonHTMLAttributes, ReactNode } from "react";

import { Icon, type IconName } from "./Icon";

export type ButtonVariant = "primary" | "secondary" | "ghost" | "danger" | "danger-outline";

type CommonProps = {
  variant?: ButtonVariant;
  size?: "sm" | "md";
  icon?: IconName;
  trailingIcon?: IconName;
  busy?: boolean;
  block?: boolean;
  children?: ReactNode;
};

function classes({ variant = "secondary", size = "md", block }: CommonProps, extra?: string): string {
  return [
    "btn",
    `btn-${variant}`,
    size === "sm" ? "btn-sm" : "",
    block ? "btn-block" : "",
    extra || "",
  ]
    .filter(Boolean)
    .join(" ");
}

type ButtonProps = CommonProps & ButtonHTMLAttributes<HTMLButtonElement>;

export function Button({
  variant,
  size,
  icon,
  trailingIcon,
  busy,
  block,
  children,
  className,
  disabled,
  type = "button",
  ...rest
}: ButtonProps) {
  return (
    <button
      type={type}
      className={classes({ variant, size, block }, className)}
      disabled={disabled || busy}
      {...rest}
    >
      {busy ? <Icon name="refresh" className="spin" /> : icon ? <Icon name={icon} /> : null}
      {children}
      {trailingIcon && !busy ? <Icon name={trailingIcon} /> : null}
    </button>
  );
}

type LinkButtonProps = CommonProps & AnchorHTMLAttributes<HTMLAnchorElement>;

export function ButtonLink({
  variant,
  size,
  icon,
  trailingIcon,
  block,
  children,
  className,
  ...rest
}: LinkButtonProps) {
  return (
    <a className={classes({ variant, size, block }, className)} {...rest}>
      {icon ? <Icon name={icon} /> : null}
      {children}
      {trailingIcon ? <Icon name={trailingIcon} /> : null}
    </a>
  );
}

type IconButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  icon: IconName;
  // Icon-only controls carry their whole accessible name here.
  label: string;
  bordered?: boolean;
  size?: "sm" | "md";
};

export function IconButton({
  icon,
  label,
  bordered,
  size = "md",
  className,
  type = "button",
  ...rest
}: IconButtonProps) {
  return (
    <button
      type={type}
      className={["icon-btn", bordered ? "icon-btn-bordered" : "", size === "sm" ? "icon-btn-sm" : "", className || ""]
        .filter(Boolean)
        .join(" ")}
      aria-label={label}
      title={label}
      {...rest}
    >
      <Icon name={icon} size={size === "sm" ? 14 : 16} />
    </button>
  );
}
