import { Injectable } from '@nestjs/common';
import { MessagingProvider, SendRequest, SendResult, SessionHealth, SessionRecord } from './messaging-provider';

@Injectable()
export class MockMessagingProvider implements MessagingProvider {
  async send(request: SendRequest): Promise<SendResult> {
    return {
      accepted: true,
      providerMessageId: `mock-${request.idempotencyKey}`,
      acceptedAt: new Date().toISOString()
    };
  }
  async health(sessionId: string): Promise<SessionHealth> {
    return { ready: Boolean(sessionId), status: sessionId ? 'READY' : 'UNKNOWN', checkedAt: new Date().toISOString() };
  }
  async getSession(sessionId: string): Promise<SessionRecord> { return { id: sessionId, name: sessionId, status: 'ready', engineLoaded: true }; }
  async createSession(name: string): Promise<SessionRecord> { return { id: name, name, status: 'created', engineLoaded: false }; }
  async startSession(sessionId: string): Promise<SessionRecord> { return { id: sessionId, name: sessionId, status: 'ready', engineLoaded: true }; }
  async stopSession(sessionId: string): Promise<SessionRecord> { return { id: sessionId, name: sessionId, status: 'disconnected', engineLoaded: false }; }
  async logoutSession(sessionId: string): Promise<SessionRecord> { return { id: sessionId, name: sessionId, status: 'created', engineLoaded: false }; }
  async deleteSession(_sessionId: string): Promise<void> {}
  async qr(sessionId: string): Promise<unknown> { return { sessionId, qrCode: 'mock', status: 'qr_ready' }; }
  async pairingCode(sessionId: string, _phoneNumber: string): Promise<unknown> { return { sessionId, pairingCode: 'MOCK1234', status: 'authenticating' }; }
}
