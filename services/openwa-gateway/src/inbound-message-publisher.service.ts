import { Injectable, ServiceUnavailableException } from '@nestjs/common';
import { createHmac } from 'node:crypto';
import { InboundMessageOutboxService } from './inbound-message-outbox.service';
import { GatewayObservabilityService } from './observability.service';

export type InboundMessageEvent = {
  schemaVersion: '1.0';
  eventId: string;
  sessionId: string;
  clientReference?: string;
  providerMessageId?: string;
  quotedProviderMessageId?: string;
  senderMsisdn?: string;
  messageText: string;
  occurredAt: string;
};

@Injectable()
export class InboundMessagePublisherService {
  constructor(private readonly outbox: InboundMessageOutboxService, private readonly observability: GatewayObservabilityService) { this.outbox.setSender(event => this.deliver(event)); }

  async queue(event: InboundMessageEvent): Promise<void> {
    validateInbound(event);
    await this.outbox.enqueue(event);
  }

  async publish(event: InboundMessageEvent): Promise<void> {
    await this.queue(event);
    await this.deliver(event);
    await this.outbox.acknowledge(event.eventId);
  }

  private async deliver(event: InboundMessageEvent): Promise<void> {
    const target = process.env.CONTROL_API_INBOUND_URL?.trim();
    const secret = process.env.GATEWAY_CALLBACK_SECRET ?? '';
    if (!target || Buffer.byteLength(secret) < 32) throw new ServiceUnavailableException('signed inbound callback publishing is not configured');
    const body = JSON.stringify(event);
    const timestamp = Math.floor(Date.now() / 1000).toString();
    const signature = createHmac('sha256', secret).update(timestamp).update('.').update(body).digest('hex');
    const response = await fetch(target, {
      method: 'POST',
      headers: { 'content-type': 'application/json', 'x-gateway-timestamp': timestamp, 'x-gateway-signature': `sha256=${signature}`, ...(this.observability.traceparent() ? { traceparent: this.observability.traceparent()! } : {}) },
      body,
      redirect: 'manual',
      signal: AbortSignal.timeout(boundedInteger(process.env.CALLBACK_TIMEOUT_MS, 5_000, 250, 30_000))
    });
    if (!response.ok) {
      const detail = (await response.text()).replace(/[\r\n\t]+/g, ' ').slice(0, 300);
      throw new ServiceUnavailableException(`control-plane inbound callback failed with HTTP ${response.status}: ${detail}`);
    }
  }
}

function validateInbound(event: InboundMessageEvent) {
  if (event.schemaVersion !== '1.0') throw new TypeError('unsupported inbound schema version');
  if (!/^[A-Za-z0-9._:-]{1,200}$/.test(event.eventId)) throw new TypeError('eventId is invalid');
  if (!/^[A-Za-z0-9._:-]{1,200}$/.test(event.sessionId)) throw new TypeError('sessionId is invalid');
  if (!event.messageText?.trim() || event.messageText.length > 4096) throw new TypeError('messageText is invalid');
  if (event.clientReference && event.clientReference.length > 200) throw new TypeError('clientReference is invalid');
  if (event.providerMessageId && event.providerMessageId.length > 500) throw new TypeError('providerMessageId is invalid');
  if (event.quotedProviderMessageId && event.quotedProviderMessageId.length > 500) throw new TypeError('quotedProviderMessageId is invalid');
  if (event.senderMsisdn && !/^\+[1-9]\d{7,14}$/.test(event.senderMsisdn)) throw new TypeError('senderMsisdn is invalid');
  if (!event.clientReference && !event.quotedProviderMessageId && !event.senderMsisdn) throw new TypeError('recipient correlation evidence is required');
  if (Number.isNaN(Date.parse(event.occurredAt))) throw new TypeError('occurredAt is invalid');
}
function boundedInteger(value: string | undefined, fallback: number, minimum: number, maximum: number): number {
  const parsed = Number.parseInt(value ?? '', 10);
  return Number.isInteger(parsed) && parsed >= minimum && parsed <= maximum ? parsed : fallback;
}
