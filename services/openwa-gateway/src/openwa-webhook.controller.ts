import { BadRequestException, Controller, Headers, Post, Req, UnauthorizedException } from '@nestjs/common';
import type { Request } from 'express';
import { createHmac, timingSafeEqual } from 'node:crypto';
import { ProviderEventPublisherService } from './provider-event-publisher.service';

type RawRequest = Request & { rawBody?: Buffer };
type OpenWAEnvelope = {
  event?: string;
  timestamp?: string;
  sessionId?: string;
  idempotencyKey?: string;
  deliveryId?: string;
  data?: Record<string, unknown>;
};

@Controller('/internal/openwa')
export class OpenWAWebhookController {
  constructor(private readonly publisher: ProviderEventPublisherService) {}

  @Post('/events')
  async ingest(
    @Req() request: RawRequest,
    @Headers('x-openwa-signature') signature: string | undefined,
    @Headers('x-openwa-idempotency-key') headerIdempotencyKey: string | undefined
  ) {
    const raw = request.rawBody ?? Buffer.alloc(0);
    verifyOpenWASignature(raw, signature, process.env.OPENWA_WEBHOOK_SECRET ?? '');
    const envelope = request.body as OpenWAEnvelope;
    validateEnvelope(envelope, headerIdempotencyKey);
    const mapped = mapEvent(envelope);
    if (!mapped) return { accepted: true, ignored: true };
    await this.publisher.publish(this.publisher.create({
      eventId: String(envelope.idempotencyKey || headerIdempotencyKey),
      eventType: mapped.eventType,
      sessionId: String(envelope.sessionId),
      clientReference: mapped.clientReference,
      providerMessageId: mapped.providerMessageId,
      occurredAt: mapped.occurredAt,
      errorCode: mapped.errorCode,
      errorDetail: mapped.errorDetail
    }));
    return { accepted: true, ignored: false };
  }
}

function mapEvent(envelope: OpenWAEnvelope): {
  eventType: 'message.sent' | 'message.delivered' | 'message.read' | 'message.failed_retryable' | 'message.failed_permanent' | 'message.unknown';
  providerMessageId?: string;
  clientReference?: string;
  occurredAt: string;
  errorCode?: string;
  errorDetail?: string;
} | null {
  const event = String(envelope.event ?? '');
  const data = envelope.data ?? {};
  const occurredAt = eventTime(envelope.timestamp, data.timestamp);
  const providerMessageId = cleanId(data.messageId ?? data.id);
  const clientReference = cleanId(data.clientReference);
  if (event === 'message.sent') {
    if (!providerMessageId) return null;
    return { eventType: 'message.sent', providerMessageId, clientReference, occurredAt };
  }
  if (event === 'message.ack') {
    const status = String(data.status ?? '').toLowerCase();
    if (!providerMessageId) return null;
    if (status === 'sent' || status === 'pending') return { eventType: 'message.sent', providerMessageId, clientReference, occurredAt };
    if (status === 'delivered') return { eventType: 'message.delivered', providerMessageId, clientReference, occurredAt };
    if (status === 'read') return { eventType: 'message.read', providerMessageId, clientReference, occurredAt };
    if (status === 'failed') return { eventType: 'message.failed_permanent', providerMessageId, clientReference, occurredAt, errorCode: 'OPENWA_ACK_FAILED', errorDetail: 'OpenWA reported a failed acknowledgement' };
    return null;
  }
  if (event === 'message.failed') {
    if (!providerMessageId) return null;
    return { eventType: 'message.failed_permanent', providerMessageId, clientReference, occurredAt, errorCode: 'OPENWA_MESSAGE_FAILED', errorDetail: 'OpenWA reported a failed outbound message' };
  }
  return null;
}

function verifyOpenWASignature(raw: Buffer, signature: string | undefined, secret: string) {
  if (Buffer.byteLength(secret) < 32) throw new UnauthorizedException('OpenWA webhook verification is not configured');
  const expected = `sha256=${createHmac('sha256', secret).update(raw).digest('hex')}`;
  const supplied = String(signature ?? '');
  const a = Buffer.from(expected); const b = Buffer.from(supplied);
  if (a.length !== b.length || !timingSafeEqual(a, b)) throw new UnauthorizedException('invalid OpenWA webhook signature');
}

function validateEnvelope(envelope: OpenWAEnvelope, headerIdempotencyKey: string | undefined) {
  if (!envelope || typeof envelope !== 'object') throw new BadRequestException('invalid OpenWA webhook envelope');
  if (!/^[A-Za-z0-9._:-]{1,200}$/.test(String(envelope.sessionId ?? ''))) throw new BadRequestException('invalid OpenWA sessionId');
  const idempotencyKey = String(envelope.idempotencyKey || headerIdempotencyKey || '');
  if (!idempotencyKey || idempotencyKey.length > 300) throw new BadRequestException('invalid OpenWA idempotency key');
  if (headerIdempotencyKey && envelope.idempotencyKey && headerIdempotencyKey !== envelope.idempotencyKey) throw new BadRequestException('OpenWA idempotency evidence mismatch');
}

function eventTime(envelopeTimestamp: unknown, dataTimestamp: unknown): string {
  if (typeof envelopeTimestamp === 'string' && !Number.isNaN(Date.parse(envelopeTimestamp))) return new Date(envelopeTimestamp).toISOString();
  if (typeof dataTimestamp === 'number' && Number.isFinite(dataTimestamp) && dataTimestamp > 0) return new Date(dataTimestamp * 1000).toISOString();
  return new Date().toISOString();
}
function cleanId(value: unknown): string | undefined {
  const text = String(value ?? '').trim();
  return text && text.length <= 500 ? text : undefined;
}
