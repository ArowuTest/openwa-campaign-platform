import { ConflictException, Injectable, OnModuleInit, ServiceUnavailableException } from '@nestjs/common';
import { createHash } from 'node:crypto';
import { mkdir, readFile, rename, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import type { SendRequest, SendResult } from './provider/messaging-provider';

type StoredEntry = {
  fingerprint: string;
  state: 'PENDING' | 'COMPLETED' | 'UNKNOWN';
  createdAt: string;
  completedAt?: string;
  result?: SendResult;
};

type MemoryEntry = { fingerprint: string; promise: Promise<SendResult> };

@Injectable()
export class IdempotencyService implements OnModuleInit {
  private readonly entries = new Map<string, MemoryEntry>();
  private readonly directory = String(process.env.GATEWAY_IDEMPOTENCY_DIR ?? '/data/gateway-idempotency');
  private readonly maximumEntries = positiveInteger(process.env.IDEMPOTENCY_MAX_ENTRIES, 1_000_000);

  async onModuleInit() { await mkdir(this.directory, { recursive: true, mode: 0o700 }); }

  async execute(request: SendRequest, operation: () => Promise<SendResult>): Promise<SendResult> {
    const fingerprint = requestFingerprint(request);
    const existingMemory = this.entries.get(request.idempotencyKey);
    if (existingMemory) {
      if (existingMemory.fingerprint !== fingerprint) throw new ConflictException('idempotency key was already used for a different request');
      return existingMemory.promise.then(result => ({ ...result, duplicate: true }));
    }
    if (this.entries.size >= this.maximumEntries) throw new ServiceUnavailableException('idempotency store capacity is exhausted');

    const stored = await this.read(request.idempotencyKey);
    if (stored) {
      if (stored.fingerprint !== fingerprint) throw new ConflictException('idempotency key was already used for a different request');
      if (stored.state === 'COMPLETED' && stored.result) return { ...stored.result, duplicate: true };
      throw new ConflictException('an earlier gateway submission has an unresolved outcome and will not be repeated automatically');
    }

    const pending: StoredEntry = { fingerprint, state: 'PENDING', createdAt: new Date().toISOString() };
    await this.create(request.idempotencyKey, pending);
    const promise = Promise.resolve()
      .then(operation)
      .then(async result => {
        const complete: StoredEntry = { ...pending, state: 'COMPLETED', completedAt: new Date().toISOString(), result: { ...result, duplicate: false } };
        await this.replace(request.idempotencyKey, complete);
        return { ...result, duplicate: false };
      })
      .catch(async error => {
        const unknown: StoredEntry = { ...pending, state: 'UNKNOWN', completedAt: new Date().toISOString() };
        await this.replace(request.idempotencyKey, unknown).catch(() => undefined);
        throw error;
      })
      .finally(() => this.entries.delete(request.idempotencyKey));
    this.entries.set(request.idempotencyKey, { fingerprint, promise });
    return promise;
  }

  async reconcileUnknown(idempotencyKey: string, expectedFingerprint: string, result: SendResult): Promise<void> {
    const stored = await this.read(idempotencyKey);
    if (!stored || stored.state !== 'UNKNOWN') throw new ConflictException('only unresolved idempotency records may be reconciled');
    if (stored.fingerprint !== expectedFingerprint) throw new ConflictException('reconciliation evidence does not match the original submission');
    await this.replace(idempotencyKey, { ...stored, state: 'COMPLETED', completedAt: new Date().toISOString(), result: { ...result, duplicate: false } });
  }

  private async create(key: string, entry: StoredEntry) {
    await mkdir(this.directory, { recursive: true, mode: 0o700 });
    const path = this.path(key);
    try {
      await writeFile(path, JSON.stringify(entry), { encoding: 'utf8', mode: 0o600, flag: 'wx' });
    } catch (error) {
      if ((error as NodeJS.ErrnoException)?.code === 'EEXIST') throw new ConflictException('idempotency record already exists');
      throw error;
    }
  }
  private async replace(key: string, entry: StoredEntry) {
    const path = this.path(key); const temporary = `${path}.${process.pid}.${Date.now()}.tmp`;
    await writeFile(temporary, JSON.stringify(entry), { encoding: 'utf8', mode: 0o600, flag: 'wx' });
    await rename(temporary, path);
  }
  private async read(key: string): Promise<StoredEntry | undefined> {
    try { return JSON.parse(await readFile(this.path(key), 'utf8')) as StoredEntry; }
    catch (error) { if ((error as NodeJS.ErrnoException)?.code === 'ENOENT') return undefined; throw error; }
  }
  private path(key: string): string { return join(this.directory, `${safeName(key)}.json`); }
}

function requestFingerprint(request: SendRequest): string {
  const canonical = JSON.stringify({
    provider: request.provider,
    engine: request.engine,
    gatewayPoolId: request.gatewayPoolId,
    gatewayPoolVersion: request.gatewayPoolVersion,
    gatewayAdapterVersion: request.gatewayAdapterVersion,
    sessionId: request.sessionId,
    routeReference: request.routeReference,
    recipientMsisdn: request.recipientMsisdn,
    messageType: request.messageType,
    body: request.body ?? '',
    mediaUrl: request.mediaUrl ?? '',
    clientReference: request.clientReference ?? ''
  });
  return createHash('sha256').update(canonical).digest('hex');
}
function safeName(value: string): string {
  if (!/^[A-Za-z0-9:_-]{16,200}$/.test(value)) throw new TypeError('idempotency key format is invalid');
  return Buffer.from(value).toString('base64url');
}
function positiveInteger(value: string | undefined, fallback: number): number {
  const parsed = Number.parseInt(value ?? '', 10);
  return Number.isInteger(parsed) && parsed > 0 ? parsed : fallback;
}
