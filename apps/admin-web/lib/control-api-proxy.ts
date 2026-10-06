import { isIP } from 'node:net';

const DEFAULT_DEVELOPMENT_CONTROL_API = 'http://127.0.0.1:8080/';

type ProxyEnvironment = Record<string, string | undefined>;
type FetchLike = (input: string | URL | Request, init?: RequestInit) => Promise<Response>;

const REQUEST_HEADERS_TO_STRIP = new Set([
  'connection',
  'forwarded',
  'host',
  'keep-alive',
  'proxy-authenticate',
  'proxy-authorization',
  'te',
  'trailer',
  'transfer-encoding',
  'upgrade',
  'x-forwarded-for',
  'x-forwarded-host',
  'x-forwarded-port',
  'x-forwarded-proto',
  'x-railway-edge',
  'x-real-ip'
]);

const RESPONSE_HEADERS_TO_STRIP = new Set([
  'connection',
  'keep-alive',
  'proxy-authenticate',
  'proxy-authorization',
  'te',
  'trailer',
  'transfer-encoding',
  'upgrade'
]);

export function resolveControlAPIBase(env: ProxyEnvironment = process.env): URL {
  const production = String(env.NODE_ENV ?? '').toLowerCase() === 'production';
  const configured = String(env.ADMIN_WEB_CONTROL_API_URL ?? '').trim();

  if (!configured) {
    if (production) {
      throw new Error('production admin-web requires ADMIN_WEB_CONTROL_API_URL');
    }
    return new URL(DEFAULT_DEVELOPMENT_CONTROL_API);
  }

  let parsed: URL;
  try {
    parsed = new URL(configured);
  } catch {
    throw new Error('ADMIN_WEB_CONTROL_API_URL must be a valid HTTP(S) origin');
  }

  if (!['http:', 'https:'].includes(parsed.protocol)) {
    throw new Error('ADMIN_WEB_CONTROL_API_URL must use http or https');
  }
  if (parsed.username || parsed.password) {
    throw new Error('ADMIN_WEB_CONTROL_API_URL must not contain credentials');
  }
  if ((parsed.pathname && parsed.pathname !== '/') || parsed.search || parsed.hash) {
    throw new Error('ADMIN_WEB_CONTROL_API_URL must be an origin without path, query or fragment');
  }

  const hostname = parsed.hostname.toLowerCase().replace(/\.$/, '');
  if (production && !hostname.endsWith('.railway.internal')) {
    throw new Error('production ADMIN_WEB_CONTROL_API_URL must use Railway private DNS (*.railway.internal)');
  }

  return new URL(`${parsed.protocol}//${parsed.host}/`);
}

export function buildControlAPIURL(
  requestURL: string,
  pathSegments: readonly string[],
  env: ProxyEnvironment = process.env
): URL {
  const base = resolveControlAPIBase(env);
  const encoded = pathSegments.map((segment) => encodeURIComponent(segment)).join('/');
  const target = new URL(`api/${encoded}`, base);
  target.search = new URL(requestURL).search;
  return target;
}

function isProduction(env: ProxyEnvironment): boolean {
  return String(env.NODE_ENV ?? '').toLowerCase() === 'production';
}

function railwayClientContext(input: Headers): { edge: string; clientIP: string } | undefined {
  const edge = String(input.get('x-railway-edge') ?? '').trim();
  const clientIP = String(input.get('x-real-ip') ?? '').trim();
  if (!edge || edge.length > 128 || isIP(clientIP) === 0) return undefined;
  return { edge, clientIP };
}

function forwardRequestHeaders(input: Headers, env: ProxyEnvironment): Headers | undefined {
  const output = new Headers();
  input.forEach((value, name) => {
    if (REQUEST_HEADERS_TO_STRIP.has(name.toLowerCase())) return;
    output.append(name, value);
  });

  if (isProduction(env)) {
    const network = railwayClientContext(input);
    if (!network) return undefined;
    output.set('X-Railway-Edge', network.edge);
    output.set('X-Real-IP', network.clientIP);
  }

  return output;
}

function forwardResponseHeaders(input: Headers): Headers {
  const output = new Headers();
  input.forEach((value, name) => {
    const lower = name.toLowerCase();
    if (RESPONSE_HEADERS_TO_STRIP.has(lower) || lower === 'set-cookie') return;
    output.append(name, value);
  });

  const cookieReader = (input as Headers & { getSetCookie?: () => string[] }).getSetCookie;
  if (typeof cookieReader === 'function') {
    for (const cookie of cookieReader.call(input)) output.append('set-cookie', cookie);
  } else {
    const cookie = input.get('set-cookie');
    if (cookie) output.append('set-cookie', cookie);
  }
  return output;
}

function unavailableResponse(): Response {
  return Response.json(
    { code: 'CONTROL_API_UNAVAILABLE', message: 'The control API is temporarily unavailable.' },
    {
      status: 503,
      headers: {
        'cache-control': 'no-store'
      }
    }
  );
}

function networkContextUnavailableResponse(): Response {
  return Response.json(
    { code: 'NETWORK_CONTEXT_UNAVAILABLE', message: 'Trusted Railway client network context is required.' },
    {
      status: 403,
      headers: {
        'cache-control': 'no-store'
      }
    }
  );
}

export async function proxyControlAPI(
  request: Request,
  pathSegments: readonly string[],
  fetchFn: FetchLike = fetch,
  env: ProxyEnvironment = process.env
): Promise<Response> {
  let target: URL;
  try {
    target = buildControlAPIURL(request.url, pathSegments, env);
  } catch {
    return unavailableResponse();
  }

  const method = request.method.toUpperCase();
  const headers = forwardRequestHeaders(request.headers, env);
  if (!headers) return networkContextUnavailableResponse();

  const init: RequestInit & { duplex?: 'half' } = {
    method,
    headers,
    cache: 'no-store',
    redirect: 'manual',
    signal: AbortSignal.timeout(120_000)
  };

  if (method !== 'GET' && method !== 'HEAD' && request.body) {
    init.body = request.body;
    init.duplex = 'half';
  }

  let upstream: Response;
  try {
    upstream = await fetchFn(target, init);
  } catch {
    return unavailableResponse();
  }

  return new Response(upstream.body, {
    status: upstream.status,
    statusText: upstream.statusText,
    headers: forwardResponseHeaders(upstream.headers)
  });
}
