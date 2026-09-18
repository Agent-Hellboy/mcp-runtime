import type { ReactNode } from "react";

import { Icon } from "./Icon";

export type Crumb = { label: string; onClick?: () => void };

type PageHeaderProps = {
  title: string;
  description?: ReactNode;
  breadcrumb?: Crumb[];
  actions?: ReactNode;
  titleId?: string;
};

// One header shape for every screen: optional breadcrumb, title, one line of
// explanation, and the contextual actions for this page.
export function PageHeader({ title, description, breadcrumb, actions, titleId }: PageHeaderProps) {
  return (
    <div className="page-header">
      <div className="page-header-text">
        {breadcrumb && breadcrumb.length > 0 ? (
          <nav className="breadcrumb" aria-label="Breadcrumb">
            {breadcrumb.map((crumb, index) => (
              <span key={`${crumb.label}-${index}`} className="breadcrumb-part">
                {crumb.onClick ? (
                  <button type="button" className="link-button" onClick={crumb.onClick}>
                    {crumb.label}
                  </button>
                ) : (
                  <span>{crumb.label}</span>
                )}
                {index < breadcrumb.length - 1 ? (
                  <Icon name="chevronRight" size={12} className="sep" />
                ) : null}
              </span>
            ))}
          </nav>
        ) : null}
        <h1 className="page-title" id={titleId}>
          {title}
        </h1>
        {description ? <p className="page-description">{description}</p> : null}
      </div>
      {actions ? <div className="page-actions">{actions}</div> : null}
    </div>
  );
}
