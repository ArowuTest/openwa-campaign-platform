import type { Metadata } from 'next';

import { AppShell } from '../components/app-shell';
import { AuthProvider } from '../components/auth-provider';
import './globals.css';

export const metadata: Metadata = {
  title: {
    default: 'Campaign Ops',
    template: '%s · Campaign Ops'
  },
  description: 'Internal governed WhatsApp campaign operations control plane',
  robots: { index: false, follow: false }
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en">
      <body>
        <a className="skip-link" href="#main-content">Skip to main content</a>
        <AuthProvider>
          <AppShell>{children}</AppShell>
        </AuthProvider>
      </body>
    </html>
  );
}
