import { hasPermission } from './session.ts';

export type NavigationItem = {
  label: string;
  href: string;
  permission: string;
  group: 'Operate' | 'Govern' | 'Observe';
  description: string;
};

export const navigation: readonly NavigationItem[] = [
  { label: 'Dashboard', href: '/', permission: 'operations.read', group: 'Observe', description: 'Live platform health and operational exceptions' },
  { label: 'Organisations', href: '/organisations', permission: 'organisation.read', group: 'Govern', description: 'Internal client records and governance state' },
  { label: 'Consent reviews', href: '/consent-reviews', permission: 'consent.read', group: 'Govern', description: 'Offline consent evidence and maker-checker decisions' },
  { label: 'Audience imports', href: '/audiences/imports', permission: 'audience.read', group: 'Operate', description: 'Preview, reconcile and approve governed audience intake' },
  { label: 'Audience builder', href: '/audiences/builder', permission: 'audience.read', group: 'Operate', description: 'Build governed cohorts and immutable snapshots' },
  { label: 'Campaigns', href: '/campaigns', permission: 'campaign.read', group: 'Operate', description: 'Prepare, test, approve and execute campaigns' },
  { label: 'Senders', href: '/senders', permission: 'sender.read', group: 'Operate', description: 'Pools, sessions, gateway nodes and capacity' },
  { label: 'Operations', href: '/operations', permission: 'operations.read', group: 'Observe', description: 'Incidents, UNKNOWN reconciliation and durable jobs' },
  { label: 'Reports', href: '/reports', permission: 'report.read', group: 'Observe', description: 'Privacy-aware campaign and operational reporting' }
] as const;

const authPaths = ['/login', '/mfa', '/step-up', '/access-denied'];

export function availableNavigation(permissions: readonly string[] | undefined): NavigationItem[] {
  return navigation.filter((item) => hasPermission(permissions, item.permission));
}

export function permissionForPath(pathname: string): string | undefined {
  if (!pathname || authPaths.some((path) => pathname === path || pathname.startsWith(`${path}/`))) return undefined;
  const match = [...navigation]
    .filter((item) => item.href === '/' ? pathname === '/' : pathname === item.href || pathname.startsWith(`${item.href}/`))
    .sort((left, right) => right.href.length - left.href.length)[0];
  return match?.permission;
}

export function defaultLandingPath(permissions: readonly string[] | undefined): string {
  if (hasPermission(permissions, 'operations.read')) return '/';
  const priority = ['/campaigns', '/audiences/imports', '/organisations', '/consent-reviews', '/senders', '/reports'];
  const available = availableNavigation(permissions);
  for (const href of priority) {
    if (available.some((item) => item.href === href)) return href;
  }
  return available[0]?.href ?? '/access-denied';
}
