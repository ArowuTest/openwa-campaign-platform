import { Injectable, ServiceUnavailableException } from '@nestjs/common';
import { ProviderEventOutboxService } from './provider-event-outbox.service';
import { createHmac, randomUUID } from 'node:crypto';
import { GatewayObservabilityService } from './observability.service';

type ProviderEventType =
  | 'gateway.accepted'
  | 'message.sent'
  | 'message.delivered'
  | 'message.read'
  | 'message.failed_retryable'
  | 'message.failed_permanent'
  | 'message.unknown';

export type ProviderMessageEvent = {
  schemaVersion: '1.0';
  eventId: string;
  eventType: ProviderEventType;
  sessionId: string;
  clientReference?: string;
  providerMessageId?: string;
  occurredAt: string;
  errorCode?: string;
  errorDetail?: string;
};

// The eventual OpenWA adapter must place provider callbacks into a durable local
// event outbox and retry this publisher with the exact same eventId/body. This
// service deliberately performs one bounded attempt; it never invents a new ID
// during a retry and never logs message content or recipient MSISDNs.
@Injectable()
export class ProviderEventPublisherService {
  constructor(private readonly outbox: ProviderEventOutboxService, private readonly observability: GatewayObservabilityService) { this.outbox.setSender(event => this.deliver(event)); }
  configured(): boolean {
    return Boolean(process.env.CONTROL_API_CALLBACK_URL && process.env.GATEWAY_CALLBACK_SECRET);
  }

  create(input: Omit<ProviderMessageEvent, 'schemaVersion' | 'eventId' | 'occurredAt'> & { occurredAt?: string; eventId?: string }): ProviderMessageEvent {
    return {
      schemaVersion: '1.0',
      eventId: input.eventId ?? randomUUID(),
      eventType: input.eventType,
      sessionId: input.sessionId,
      clientReference: input.clientReference,
      providerMessageId: input.providerMessageId,
      occurredAt: input.occurredAt ?? new Date().toISOString(),
      errorCode: input.errorCode,
      errorDetail: input.errorDetail
    };
  }

  async publish(event: ProviderMessageEvent): Promise<void> {
    validateEvent(event);
    await this.outbox.enqueue(event);
    await this.deliver(event);
    await this.outbox.acknowledge(event.eventId);
  }

  private async deliver(event: ProviderMessageEvent): Promise<void> {
    const target = process.env.CONTROL_API_CALLBACK_URL?.trim();
    const secret = process.env.GATEWAY_CALLBACK_SECRET ?? '';
    if (!target || Buffer.byteLength(secret) < 32) {
      throw new ServiceUnavailableException('signed provider callback publishing is not configured');
    }
    const body = JSON.stringify(event);
    const timestamp = Math.floor(Date.now() / 1000).toString();
    const signature = createHmac('sha256', secret).update(timestamp).update('.').update(body).digest('hex');
    const timeoutMs = boundedInteger(process.env.CALLBACK_TIMEOUT_MS, 5_000, 250, 30_000);
    const response = await fetch(target, {
      method: 'POST',
      headers: {
        'content-type': 'application/json',
        'x-gateway-timestamp': timestamp,
        'x-gateway-signature': `sha256=${signature}`,
        ...(this.observability.traceparent() ? { traceparent: this.observability.traceparent()! } : {})
      },
      body,
      signal: AbortSignal.timeout(timeoutMs)
    });
    if (!response.ok) {
      // Never consume an unbounded or potentially sensitive response body.
      const detail = (await response.text()).slice(0, 300);
      throw new ServiceUnavailableException(`control-plane callback failed with HTTP ${response.status}: ${detail}`);
    }
  }
}

function validateEvent(event: ProviderMessageEvent) {
  if (event.schemaVersion !== '1.0') throw new TypeError('unsupported provider-event schema version');
  if (!event.eventId?.trim() || event.eventId.length > 200) throw new TypeError('eventId is invalid');
  if (!event.sessionId?.trim() || event.sessionId.length > 200) throw new TypeError('sessionId is invalid');
  if (event.clientReference && event.clientReference.length > 200) throw new TypeError('clientReference is invalid');
  if (event.providerMessageId && event.providerMessageId.length > 500) throw new TypeError('providerMessageId is invalid');
  const acknowledgements: ProviderEventType[] = ['gateway.accepted', 'message.sent', 'message.delivered', 'message.read'];
  const failures: ProviderEventType[] = ['message.failed_retryable', 'message.failed_permanent', 'message.unknown'];
  if (acknowledgements.includes(event.eventType) && !event.providerMessageId?.trim()) throw new TypeError('providerMessageId is required for acknowledgement events');
  if (failures.includes(event.eventType) && !event.errorCode?.trim()) throw new TypeError('errorCode is required for failure or unknown events');
  if (!event.clientReference?.trim() && !event.providerMessageId?.trim()) throw new TypeError('clientReference or providerMessageId is required');
  if (Number.isNaN(Date.parse(event.occurredAt))) throw new TypeError('occurredAt is invalid');
}

function boundedInteger(value: string | undefined, fallback: number, minimum: number, maximum: number): number {
  const parsed = Number.parseInt(value ?? '', 10);
  return Number.isInteger(parsed) && parsed >= minimum && parsed <= maximum ? parsed : fallback;
}
