'use client';

import Link from 'next/link';
import { usePathname, useRouter } from 'next/navigation';
import { useEffect, useMemo, useState } from 'react';

import { availableNavigation, defaultLandingPath, permissionForPath } from '../lib/navigation';
import { hasPermission } from '../lib/session';
import { useAuth } from './auth-provider';

const publicPaths = new Set(['/login', '/mfa']);

function environmentLabel(value?: string) {
  const normalized = (value || 'unknown').trim().toUpperCase();
  return normalized || 'UNKNOWN';
}

export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();
  const { status, session, error, logout } = useAuth();
  const [signingOut, setSigningOut] = useState(false);
  const publicRoute = publicPaths.has(pathname);
  const navItems = useMemo(() => availableNavigation(session?.permissions), [session?.permissions]);
  const requiredPermission = permissionForPath(pathname);
  const authorised = !requiredPermission || hasPermission(session?.permissions, requiredPermission);

  useEffect(() => {
    if (publicRoute || pathname === '/access-denied' || pathname === '/step-up') return;
    if (status === 'anonymous') {
      const returnTo = encodeURIComponent(pathname || '/');
      router.replace(`/login?returnTo=${returnTo}`);
      return;
    }
    if (status === 'authenticated' && !authorised && requiredPermission) {
      router.replace(`/access-denied?permission=${encodeURIComponent(requiredPermission)}`);
    }
  }, [authorised, pathname, publicRoute, requiredPermission, router, status]);

  if (publicRoute) return <div className="auth-surface">{children}</div>;

  if (status === 'loading') {
    return (
      <div className="boot-screen" role="status" aria-live="polite">
        <div className="boot-card"><span className="spinner" aria-hidden="true" /><strong>Loading operator context</strong><small>Verifying session and permissions…</small></div>
      </div>
    );
  }

  if (status === 'anonymous') {
    return (
      <div className="boot-screen" role="status" aria-live="polite">
        <div className="boot-card"><strong>Authentication required</strong><small>{error || 'Redirecting to secure sign in…'}</small></div>
      </div>
    );
  }

  if (!session) return null;

  const activeHref = [...navItems]
    .filter((item) => item.href === '/' ? pathname === '/' : pathname === item.href || pathname.startsWith(`${item.href}/`))
    .sort((a, b) => b.href.length - a.href.length)[0]?.href;

  async function handleLogout() {
    if (signingOut) return;
    setSigningOut(true);
    try {
      await logout();
      router.replace('/login');
    } catch {
      setSigningOut(false);
    }
  }

  return (
    <div className="operator-frame">
      <div className="classification-bar" role="status" aria-label="Environment and data classification">
        <span>{environmentLabel(session.environment)}</span>
        <span aria-hidden="true">•</span>
        <strong>{environmentLabel(session.classification)}</strong>
        <span aria-hidden="true">•</span>
        <span>{session.scopeType === 'PLATFORM' ? 'Platform-wide operator scope' : 'Restricted organisation scope'}</span>
      </div>

      <aside className="sidebar" aria-label="Primary navigation">
        <Link className="brand" href={defaultLandingPath(session.permissions)} aria-label="Campaign Ops home">
          <span className="brand-mark" aria-hidden="true">CO</span>
          <span><strong>Campaign Ops</strong><small>Internal control plane</small></span>
        </Link>
        <nav className="sidebar-nav">
          {(['Operate', 'Govern', 'Observe'] as const).map((group) => {
            const grouped = navItems.filter((item) => item.group === group);
            if (!grouped.length) return null;
            return (
              <div className="nav-group" key={group}>
                <span className="nav-group-label">{group}</span>
                {grouped.map((item) => (
                  <Link className={activeHref === item.href ? 'nav-link nav-link-active' : 'nav-link'} href={item.href} key={item.href}>
                    <span>{item.label}</span>
                    <small>{item.description}</small>
                  </Link>
                ))}
              </div>
            );
          })}
        </nav>
        <div className="sidebar-footer">
          <strong>{session.displayName}</strong>
          <small>{session.email}</small>
          <button className="text-button" type="button" onClick={handleLogout} disabled={signingOut}>{signingOut ? 'Signing out…' : 'Sign out'}</button>
        </div>
      </aside>

      <div className="operator-main">
        <header className="mobile-header">
          <Link className="mobile-brand" href={defaultLandingPath(session.permissions)}><span className="brand-mark" aria-hidden="true">CO</span><strong>Campaign Ops</strong></Link>
          <details className="mobile-nav">
            <summary>Menu</summary>
            <nav aria-label="Mobile navigation">
              {navItems.map((item) => <Link href={item.href} key={item.href}>{item.label}</Link>)}
              <button type="button" onClick={handleLogout} disabled={signingOut}>Sign out</button>
            </nav>
          </details>
        </header>
        <main className="content" id="main-content" tabIndex={-1}>{children}</main>
      </div>
    </div>
  );
}
