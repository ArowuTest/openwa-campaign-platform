import { Injectable } from '@nestjs/common';
import { EmbeddedOpenWAEngineService } from './embedded-openwa-engine.service';
import type { MessagingProvider, SendRequest, SendResult, SessionHealth, SessionRecord, SessionStartOptions } from './messaging-provider';

@Injectable()
export class OpenWAProvider implements MessagingProvider {
  constructor(private readonly embedded: EmbeddedOpenWAEngineService) {}

  send(request: SendRequest): Promise<SendResult> {
    return this.embedded.send(request);
  }

  health(sessionId: string): Promise<SessionHealth> {
    return this.embedded.health(sessionId);
  }

  getSession(sessionId: string): Promise<SessionRecord> {
    return this.embedded.getSession(sessionId);
  }

  createSession(name: string): Promise<SessionRecord> {
    return this.embedded.createSession(name);
  }
  startSession(sessionId: string, options?: SessionStartOptions): Promise<SessionRecord> {
    return this.embedded.startSession(sessionId, options);
  }

  stopSession(sessionId: string): Promise<SessionRecord> {
    return this.embedded.stopSession(sessionId);
  }

  logoutSession(sessionId: string): Promise<SessionRecord> {
    return this.embedded.logoutSession(sessionId);
  }

  deleteSession(sessionId: string): Promise<void> {
    return this.embedded.deleteSession(sessionId);
  }

  qr(sessionId: string): Promise<unknown> {
    return this.embedded.qr(sessionId);
  }

  pairingCode(sessionId: string, phoneNumber: string): Promise<unknown> {
    return this.embedded.pairingCode(sessionId, phoneNumber);
  }

  listSessions(): Promise<SessionRecord[]> {
    return this.embedded.listSessions();
  }

  synchronizeSentToday(sessionId: string, sentToday: number): void {
    this.embedded.synchronizeSentToday(sessionId, sentToday);
  }
}
