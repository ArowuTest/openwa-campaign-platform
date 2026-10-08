export const browserAPIBase = '/api';

export type APIErrorEnvelope = {
  error?: string;
  code?: string;
  message?: string;
  details?: Record<string, unknown>;
  requestId?: string;
};

export class APIError extends Error {
  status: number;
  code: string;
  details?: Record<string, unknown>;
  requestId?: string;

  constructor(status: number, envelope: APIErrorEnvelope = {}) {
    super(envelope.message || `Request failed with status ${status}`);
    this.name = 'APIError';
    this.status = status;
    this.code = envelope.error || envelope.code || 'REQUEST_FAILED';
    this.details = envelope.details;
    this.requestId = envelope.requestId;
  }
}

export type ListEnvelope<T> = {
  items: T[];
  count: number;
  hasMore: boolean;
  nextCursor?: string;
};

function requestId(): string {
  if (typeof globalThis.crypto?.randomUUID === 'function') return globalThis.crypto.randomUUID();
  return `req-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

function isSafeMethod(method: string): boolean {
  return method === 'GET' || method === 'HEAD' || method === 'OPTIONS';
}

export function csrfTokenFromCookie(cookieHeader: string): string | undefined {
  for (const segment of String(cookieHeader ?? '').split(';')) {
    const [rawName, ...rawValue] = segment.trim().split('=');
    if (rawName !== 'campaign_csrf') continue;
    const value = rawValue.join('=');
    if (!value) return undefined;
    try {
      const decoded = decodeURIComponent(value).trim();
      return decoded || undefined;
    } catch {
      return undefined;
    }
  }
  return undefined;
}

export async function requestWithFetch(
  path: string,
  init: RequestInit = {},
  fetchFn: typeof fetch = fetch
): Promise<Response> {
  const method = String(init.method ?? 'GET').toUpperCase();
  const attempts = isSafeMethod(method) ? 2 : 1;
  let lastError: unknown;

  for (let attempt = 0; attempt < attempts; attempt += 1) {
    try {
      const headers = new Headers(init.headers);
      if (!headers.has('Accept')) headers.set('Accept', 'application/json');
      if (!headers.has('X-Request-ID')) headers.set('X-Request-ID', requestId());
      return await fetchFn(path, {
        ...init,
        method,
        headers,
        credentials: init.credentials ?? 'include',
        cache: init.cache ?? 'no-store'
      });
    } catch (error) {
      lastError = error;
      if (!isSafeMethod(method) || attempt + 1 >= attempts) throw error;
    }
  }

  throw lastError instanceof Error ? lastError : new Error('Request failed');
}

async function parseResponse(response: Response): Promise<unknown> {
  if (response.status === 204) return undefined;
  const contentType = response.headers.get('content-type') ?? '';
  if (!contentType.includes('application/json')) return undefined;
  return response.json().catch(() => undefined);
}

export async function apiRequest<T>(
  path: string,
  init: RequestInit = {},
  options: { csrfToken?: string; idempotencyKey?: string } = {}
): Promise<T> {
  const headers = new Headers(init.headers);
  const method = String(init.method ?? 'GET').toUpperCase();
  const cookieCSRF = typeof document !== 'undefined' ? csrfTokenFromCookie(document.cookie) : undefined;
  const csrfToken = options.csrfToken || (!isSafeMethod(method) ? cookieCSRF : undefined);
  if (csrfToken) headers.set('X-CSRF-Token', csrfToken);
  if (options.idempotencyKey) headers.set('Idempotency-Key', options.idempotencyKey);
  if (init.body && !(init.body instanceof FormData) && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json');
  }

  const response = await requestWithFetch(`${browserAPIBase}${path}`, { ...init, headers });
  const payload = await parseResponse(response);
  if (!response.ok) {
    throw new APIError(response.status, (payload && typeof payload === 'object' ? payload : {}) as APIErrorEnvelope);
  }
  return payload as T;
}

export async function collectBoundedPages<T>(
  fetchPage: (cursor?: string) => Promise<ListEnvelope<T>>,
  maxPages = 50
): Promise<T[]> {
  if (!Number.isInteger(maxPages) || maxPages < 1 || maxPages > 500) {
    throw new Error('Page limit must be between 1 and 500');
  }
  const items: T[] = [];
  const seen = new Set<string>();
  let cursor: string | undefined;

  for (let pageNumber = 0; pageNumber < maxPages; pageNumber += 1) {
    const page = await fetchPage(cursor);
    if (!page || !Array.isArray(page.items) || !Number.isInteger(page.count) || page.count < 0 || typeof page.hasMore !== 'boolean') {
      throw new Error('Malformed paginated response');
    }
    items.push(...page.items);
    if (!page.hasMore) return items;

    const next = typeof page.nextCursor === 'string' ? page.nextCursor.trim() : '';
    if (!next) throw new Error('Paginated response claims more data without a continuation cursor');
    if (seen.has(next) || next === cursor) throw new Error('Repeated cursor detected while reading bounded inventory');
    seen.add(next);
    cursor = next;
  }

  throw new Error(`Page limit exceeded while reading bounded inventory (${maxPages})`);
}
