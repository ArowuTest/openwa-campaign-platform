import { Inject, Injectable, NotImplementedException, ServiceUnavailableException } from '@nestjs/common';
import { IdempotencyService } from './idempotency.service';
import { SessionPipelineService } from './session-pipeline.service';
import { MESSAGING_PROVIDER } from './provider/provider.token';
import type { MessagingProvider, SendRequest, SendResult, SessionHealth, SessionRecord, SessionStartOptions } from './provider/messaging-provider';
import { SessionAuthorityService } from './session-authority.service';

@Injectable()
export class GatewayMessagingService {
  constructor(
    @Inject(MESSAGING_PROVIDER) private readonly provider: MessagingProvider,
    private readonly idempotency: IdempotencyService,
    private readonly pipelines: SessionPipelineService,
    private readonly authorities: SessionAuthorityService
  ) {}

  async send(request: SendRequest): Promise<SendResult> {
    await this.authorities.validate(request);
    const health = await this.provider.health(request.sessionId);
    if (!health.ready || health.status !== 'READY') throw new ServiceUnavailableException('session is not ready for governed submission');
    return this.pipelines.run(request.sessionId, () =>
      this.authorities.submitIfCurrent(request, async () => {
        const currentHealth = await this.provider.health(request.sessionId);
        if (!currentHealth.ready || currentHealth.status !== 'READY') throw new ServiceUnavailableException('session is not ready for governed submission');
        this.authorities.assertOwned(request.sessionId, request.sessionLeaseVersion);
        return this.idempotency.execute(request, async () => {
          try {
            this.authorities.assertOwned(request.sessionId, request.sessionLeaseVersion);
            const result = await this.provider.send(request);
            this.pipelines.recordProviderSuccess(request.sessionId);
            return result;
          } catch (error) {
            this.pipelines.recordProviderFailure(request.sessionId);
            throw error;
          }
        });
      })
    );
  }
  async health(sessionId: string): Promise<SessionHealth & { pipeline: ReturnType<SessionPipelineService['status']> }> {
    return { ...(await this.provider.health(sessionId)), pipeline: this.pipelines.status(sessionId) };
  }
  listSessions(): Promise<SessionRecord[]> { return required(this.provider.listSessions, 'session listing').call(this.provider); }
  synchronizeSentToday(sessionId: string, sentToday: number): void { required(this.provider.synchronizeSentToday, 'session usage synchronization').call(this.provider, sessionId, sentToday); }
  getSession(sessionId: string): Promise<SessionRecord> { return required(this.provider.getSession, 'session read').call(this.provider, sessionId); }
  async createSession(name: string): Promise<SessionRecord> {
    return this.authorities.runIfNotTombstoned(name, () =>
      required(this.provider.createSession, 'session creation').call(this.provider, name)
    );
  }
  async startSession(sessionId: string, options?: SessionStartOptions): Promise<SessionRecord> {
    return this.authorities.runIfNotTombstoned(sessionId, async () => {
      this.authorities.assertOwned(sessionId);
      this.pipelines.beginStart(sessionId);
      try {
        const session = await required(this.provider.startSession, 'session start').call(this.provider, sessionId, options);
        this.pipelines.finishStart(sessionId, true);
        return session;
      } catch (error) {
        this.pipelines.finishStart(sessionId, false);
        throw error;
      }
    });
  }
  async stopSession(sessionId: string): Promise<SessionRecord> {
    await this.pipelines.drainForTeardown(sessionId);
    try {
      return await required(this.provider.stopSession, 'session stop').call(this.provider, sessionId);
    } finally {
      this.pipelines.finishTeardown(sessionId);
    }
  }
  async logoutSession(sessionId: string): Promise<SessionRecord> {
    await this.pipelines.drainForTeardown(sessionId);
    try {
      return await required(this.provider.logoutSession, 'session logout').call(this.provider, sessionId);
    } finally {
      this.pipelines.finishTeardown(sessionId);
    }
  }
  async deleteSession(sessionId: string): Promise<void> {
    await this.authorities.tombstone(sessionId);
    await this.pipelines.drainForTeardown(sessionId);
    try {
      await required(this.provider.deleteSession, 'session deletion').call(this.provider, sessionId);
    } finally {
      this.pipelines.finishTeardown(sessionId);
      this.pipelines.forgetTornDown(sessionId);
    }
  }
  async qr(sessionId: string): Promise<unknown> {
    return this.authorities.runIfNotTombstoned(sessionId, () =>
      required(this.provider.qr, 'QR pairing').call(this.provider, sessionId)
    );
  }
  async pairingCode(sessionId: string, phoneNumber: string): Promise<unknown> {
    return this.authorities.runIfNotTombstoned(sessionId, () => {
      this.authorities.assertOwned(sessionId);
      return required(this.provider.pairingCode, 'pairing code').call(this.provider, sessionId, phoneNumber);
    });
  }
  drain(sessionId: string) { this.pipelines.drain(sessionId); }
  resume(sessionId: string) { this.pipelines.resume(sessionId); }
}

function required<T extends (...args: never[]) => unknown>(fn: T | undefined, capability: string): T {
  if (!fn) throw new NotImplementedException(`${capability} is not supported by the configured provider`);
  return fn;
}
