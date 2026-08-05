import type { Metadata } from 'next';
import Link from 'next/link';
import './globals.css';

export const metadata: Metadata = {
  title: 'Campaign Operations',
  description: 'Internal audience, consent and campaign operations portal'
};

const navigation = [
  ['Dashboard', '/'],
  ['Organisations', '/organisations'],
  ['Consent reviews', '/consent-reviews'],
  ['Audience imports', '/audiences/imports'],
  ['Audience builder', '/audiences/builder'],
  ['Campaigns', '/campaigns'],
  ['Senders', '/senders'],
  ['Reports', '/reports']
] as const;

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en">
      <body>
        <a className="skip-link" href="#main-content">Skip to main content</a>
        <div className="shell">
          <aside className="sidebar" aria-label="Primary navigation">
            <div className="brand">
              <span className="brand-mark" aria-hidden="true">CO</span>
              <div><strong>Campaign Ops</strong><small>Internal platform</small></div>
            </div>
            <nav>
              {navigation.map(([label, href]) => <Link href={href} key={href}>{label}</Link>)}
            </nav>
            <div className="sidebar-footer">
              <span className="environment-badge">Development</span>
              <small>No external client access</small>
            </div>
          </aside>
          <main className="content" id="main-content">{children}</main>
        </div>
      </body>
    </html>
  );
}
