export type SessionContext = {
  id: string;
  email: string;
  displayName: string;
  permissions: string[];
  sessionId: string;
  lastLoginAt?: string | null;
  environment?: string;
  classification?: string;
  scopeType?: 'PLATFORM' | 'ORGANISATION';
  organisationIds?: string[];
};

export function hasPermission(permissions: readonly string[] | undefined, required: string): boolean {
  if (!permissions || permissions.length === 0 || !required) return false;
  return permissions.includes('*') || permissions.includes(required);
}

export function safeReturnPath(value: string | null | undefined): string {
  const raw = String(value ?? '').trim();
  if (!raw || !raw.startsWith('/') || raw.startsWith('//')) return '/';
  if (/[\\\u0000-\u001f\u007f]/.test(raw)) return '/';

  let decoded = raw;
  try {
    for (let i = 0; i < 2; i += 1) {
      const next = decodeURIComponent(decoded);
      if (next === decoded) break;
      decoded = next;
    }
  } catch {
    return '/';
  }

  if (!decoded.startsWith('/') || decoded.startsWith('//')) return '/';
  if (/[\\\u0000-\u001f\u007f]/.test(decoded)) return '/';
  return raw;
}
