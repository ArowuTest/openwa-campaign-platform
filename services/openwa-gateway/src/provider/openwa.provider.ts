import { Injectable, ServiceUnavailableException } from '@nestjs/common';
import type { MessagingProvider, SendRequest, SendResult, SessionHealth } from './messaging-provider';

type OpenWAMessageResponse = { messageId?: string; timestamp?: number };
type OpenWASession = {
  id?: string;
  name?: string;
  status?: string;
  engineLoaded?: boolean;
  lastError?: string | null;
};

@Injectable()
export class OpenWAProvider implements MessagingProvider {
  private readonly baseUrl = requiredUrl('OPENWA_UPSTREAM_URL');
  private readonly apiKey = requiredSecret('OPENWA_UPSTREAM_API_KEY');
  private readonly timeoutMs = boundedInteger(process.env.OPENWA_UPSTREAM_TIMEOUT_MS, 30_000, 500, 120_000);

  async send(request: SendRequest): Promise<SendResult> {
    const chatId = toChatId(request.recipientMsisdn);
    const path = messagePath(request.sessionId, request.messageType);
    const body = request.messageType === 'text'
      ? { chatId, text: request.body }
      : {
          chatId,
          url: request.mediaUrl,
          caption: request.body || undefined,
          filename: request.messageType === 'document' ? safeFilename(request.mediaUrl) : undefined
        };
    const response = await this.request<OpenWAMessageResponse>('POST', path, body);
    const providerMessageId = String(response?.messageId ?? '').trim();
    if (!providerMessageId) {
      throw new ServiceUnavailableException('OpenWA accepted the request without returning a provider message identifier');
    }
    return {
      accepted: true,
      providerMessageId,
      acceptedAt: timestampToISO(response.timestamp)
    };
  }

  async health(sessionId: string): Promise<SessionHealth> {
    try {
      const session = await this.request<OpenWASession>('GET', `/api/sessions/${encodeURIComponent(sessionId)}`);
      const status = String(session?.status ?? 'unknown').toLowerCase();
      const mapped = mapSessionStatus(status);
      return {
        ready: mapped === 'READY' && session?.engineLoaded !== false,
        status: mapped,
        checkedAt: new Date().toISOString(),
        detail: safeDetail(session?.lastError)
      };
    } catch (error) {
      return {
        ready: false,
        status: 'UNKNOWN',
        checkedAt: new Date().toISOString(),
        detail: safeDetail(error)
      };
    }
  }

  async getSession(sessionId: string): Promise<OpenWASession> {
    return this.request<OpenWASession>('GET', `/api/sessions/${encodeURIComponent(sessionId)}`);
  }

  async createSession(name: string): Promise<OpenWASession> {
    const session = await this.request<OpenWASession>('POST', '/api/sessions', { name });
    await this.ensureWebhook(String(session.id || name));
    return session;
  }

  async startSession(sessionId: string): Promise<OpenWASession> {
    await this.ensureWebhook(sessionId);
    return this.request<OpenWASession>('POST', `/api/sessions/${encodeURIComponent(sessionId)}/start`);
  }

  async stopSession(sessionId: string): Promise<OpenWASession> {
    return this.request<OpenWASession>('POST', `/api/sessions/${encodeURIComponent(sessionId)}/stop`);
  }

  async logoutSession(sessionId: string): Promise<OpenWASession> {
    return this.request<OpenWASession>('POST', `/api/sessions/${encodeURIComponent(sessionId)}/logout`);
  }

  async deleteSession(sessionId: string): Promise<void> {
    await this.request<void>('DELETE', `/api/sessions/${encodeURIComponent(sessionId)}`);
  }

  async qr(sessionId: string): Promise<unknown> {
    return this.request<unknown>('GET', `/api/sessions/${encodeURIComponent(sessionId)}/qr`);
  }

  async pairingCode(sessionId: string, phoneNumber: string): Promise<unknown> {
    return this.request<unknown>('POST', `/api/sessions/${encodeURIComponent(sessionId)}/pairing-code`, { phoneNumber });
  }

  private async ensureWebhook(sessionId: string): Promise<void> {
    const target = requiredUrl('OPENWA_WEBHOOK_TARGET_URL');
    const secret = requiredSecret('OPENWA_WEBHOOK_SECRET');
    const path = `/api/sessions/${encodeURIComponent(sessionId)}/webhooks`;
    const existing = await this.request<Array<{ id?: string; url?: string; active?: boolean; events?: string[] }>>('GET', path);
    const events = ['message.sent', 'message.ack', 'message.failed'];
    const match = Array.isArray(existing) ? existing.find(item => item?.url === target) : undefined;
    if (match?.id) {
      await this.request('PUT', `${path}/${encodeURIComponent(match.id)}`, { url: target, events, secret, active: true, retryCount: 5 });
      return;
    }
    await this.request('POST', path, { url: target, events, secret, retryCount: 5 });
  }

  private async request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const response = await fetch(`${this.baseUrl}${path}`, {
      method,
      headers: {
        'content-type': 'application/json',
        'x-api-key': this.apiKey
      },
      body: body === undefined ? undefined : JSON.stringify(body),
      redirect: 'manual',
      signal: AbortSignal.timeout(this.timeoutMs)
    });
    const raw = await response.text();
    if (!response.ok) {
      const detail = safeDetail(raw) || `OpenWA returned HTTP ${response.status}`;
      const error = new ServiceUnavailableException(detail);
      (error as ServiceUnavailableException & { statusCode?: number }).statusCode = response.status;
      throw error;
    }
    if (!raw) return undefined as T;
    try {
      return JSON.parse(raw) as T;
    } catch {
      throw new ServiceUnavailableException('OpenWA returned an invalid JSON response');
    }
  }
}

function messagePath(sessionId: string, type: SendRequest['messageType']): string {
  const segment = {
    text: 'send-text',
    image: 'send-image',
    video: 'send-video',
    document: 'send-document'
  }[type];
  return `/api/sessions/${encodeURIComponent(sessionId)}/messages/${segment}`;
}

function toChatId(e164: string): string {
  const digits = e164.replace(/\D/g, '');
  if (!digits) throw new TypeError('recipient MSISDN is invalid');
  return `${digits}@c.us`;
}

function timestampToISO(value: number | undefined): string {
  if (typeof value === 'number' && Number.isFinite(value) && value > 0) {
    return new Date(value * 1000).toISOString();
  }
  return new Date().toISOString();
}

function mapSessionStatus(status: string): SessionHealth['status'] {
  switch (status) {
    case 'ready': return 'READY';
    case 'created':
    case 'initializing':
    case 'qr_ready':
    case 'authenticating': return 'PAIRING';
    case 'disconnected': return 'DISCONNECTED';
    case 'action_required': return 'RESTRICTED';
    case 'failed': return 'RESTRICTED';
    default: return 'UNKNOWN';
  }
}

function requiredUrl(name: string): string {
  const value = String(process.env[name] ?? '').trim().replace(/\/$/, '');
  if (!/^https?:\/\//.test(value)) throw new Error(`${name} must be a valid HTTP(S) URL`);
  if (process.env.NODE_ENV === 'production' && value.startsWith('http://') && process.env.OPENWA_ALLOW_INSECURE_INTERNAL !== 'true') {
    throw new Error(`${name} must use HTTPS in production unless an isolated internal network is explicitly approved`);
  }
  return value;
}

function requiredSecret(name: string): string {
  const value = String(process.env[name] ?? '');
  if (!value || (process.env.NODE_ENV === 'production' && Buffer.byteLength(value) < 32)) {
    throw new Error(`${name} is not securely configured`);
  }
  return value;
}

function safeFilename(value: string | undefined): string {
  try {
    const pathname = new URL(value ?? '').pathname;
    const name = pathname.split('/').pop() || 'document';
    return name.replace(/[^A-Za-z0-9._-]/g, '_').slice(0, 120) || 'document';
  } catch {
    return 'document';
  }
}

function safeDetail(value: unknown): string | undefined {
  const text = value instanceof Error ? value.message : String(value ?? '');
  const trimmed = text.replace(/[\r\n\t]+/g, ' ').trim();
  if (!trimmed) return undefined;
  return trimmed.slice(0, 300);
}

function boundedInteger(value: string | undefined, fallback: number, minimum: number, maximum: number): number {
  const parsed = Number.parseInt(value ?? '', 10);
  return Number.isInteger(parsed) && parsed >= minimum && parsed <= maximum ? parsed : fallback;
}
