import { BadRequestException, Body, Controller, Get, NotFoundException, Param, Post } from '@nestjs/common';
import { IdempotencyService } from './idempotency.service';
import type { SendResult } from './provider/messaging-provider';

@Controller('/v1/idempotency')
export class IdempotencyController {
  constructor(private readonly idempotency: IdempotencyService) {}

  @Get('/:key')
  async describe(@Param('key') key: string) {
    validateKey(key);
    const status = await this.idempotency.describe(key);
    if (!status) throw new NotFoundException('idempotency record was not found');
    return status;
  }

  @Post('/:key/reconcile')
  async reconcile(@Param('key') key: string, @Body() body: Record<string, unknown>) {
    validateKey(key);
    const fingerprint = String(body.expectedFingerprint ?? '').toLowerCase();
    const operatorReference = String(body.operatorReference ?? '');
    const evidenceHash = String(body.evidenceHash ?? '').toLowerCase();
    if (!/^[a-f0-9]{64}$/.test(fingerprint)) throw new BadRequestException('expectedFingerprint is invalid');
    const result = validateResult(body.result);
    await this.idempotency.reconcileUnknown(key, fingerprint, result, operatorReference, evidenceHash);
    return { status: 'reconciled' };
  }
}

function validateKey(value: string): void {
  if (!/^[A-Za-z0-9:_-]{16,200}$/.test(value)) throw new BadRequestException('idempotency key format is invalid');
}
function validateResult(value: unknown): SendResult {
  const result = value as Partial<SendResult> | undefined;
  if (!result || result.accepted !== true || typeof result.providerMessageId !== 'string' || !result.providerMessageId.trim()) throw new BadRequestException('reconciliation result is invalid');
  if (typeof result.acceptedAt !== 'string' || Number.isNaN(Date.parse(result.acceptedAt))) throw new BadRequestException('acceptedAt is invalid');
  return { accepted: true, providerMessageId: result.providerMessageId.trim(), acceptedAt: new Date(result.acceptedAt).toISOString(), duplicate: false };
}
