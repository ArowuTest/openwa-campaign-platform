import { Inject, Injectable, NotImplementedException } from '@nestjs/common';
import { IdempotencyService } from './idempotency.service';
import { SessionPipelineService } from './session-pipeline.service';
import { MESSAGING_PROVIDER } from './provider/provider.token';
import type { MessagingProvider, SendRequest, SendResult, SessionHealth, SessionRecord } from './provider/messaging-provider';

@Injectable()
export class GatewayMessagingService {
  constructor(
    @Inject(MESSAGING_PROVIDER) private readonly provider: MessagingProvider,
    private readonly idempotency: IdempotencyService,
    private readonly pipelines: SessionPipelineService
  ) {}

  send(request: SendRequest): Promise<SendResult> {
    return this.pipelines.run(request.sessionId, () =>
      this.idempotency.execute(request, () => this.provider.send(request))
    );
  }
  async health(sessionId: string): Promise<SessionHealth & { pipeline: ReturnType<SessionPipelineService['status']> }> {
    return { ...(await this.provider.health(sessionId)), pipeline: this.pipelines.status(sessionId) };
  }
  getSession(sessionId: string): Promise<SessionRecord> { return required(this.provider.getSession, 'session read').call(this.provider, sessionId); }
  createSession(name: string): Promise<SessionRecord> { return required(this.provider.createSession, 'session creation').call(this.provider, name); }
  startSession(sessionId: string): Promise<SessionRecord> { return required(this.provider.startSession, 'session start').call(this.provider, sessionId); }
  async stopSession(sessionId: string): Promise<SessionRecord> {
    this.pipelines.drain(sessionId);
    return required(this.provider.stopSession, 'session stop').call(this.provider, sessionId);
  }
  async logoutSession(sessionId: string): Promise<SessionRecord> {
    this.pipelines.drain(sessionId);
    return required(this.provider.logoutSession, 'session logout').call(this.provider, sessionId);
  }
  async deleteSession(sessionId: string): Promise<void> {
    this.pipelines.drain(sessionId);
    return required(this.provider.deleteSession, 'session deletion').call(this.provider, sessionId);
  }
  qr(sessionId: string): Promise<unknown> { return required(this.provider.qr, 'QR pairing').call(this.provider, sessionId); }
  pairingCode(sessionId: string, phoneNumber: string): Promise<unknown> { return required(this.provider.pairingCode, 'pairing code').call(this.provider, sessionId, phoneNumber); }
  drain(sessionId: string) { this.pipelines.drain(sessionId); }
  resume(sessionId: string) { this.pipelines.resume(sessionId); }
}

function required<T extends (...args: never[]) => unknown>(fn: T | undefined, capability: string): T {
  if (!fn) throw new NotImplementedException(`${capability} is not supported by the configured provider`);
  return fn;
}
