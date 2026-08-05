import type { ReactNode } from 'react';

export function PageHeader({ title, description, actions }: { title: string; description: string; actions?: ReactNode }) {
  return (
    <div className="toolbar">
      <div>
        <h1>{title}</h1>
        <p className="muted">{description}</p>
      </div>
      {actions ? <div className="toolbar-actions">{actions}</div> : null}
    </div>
  );
}
