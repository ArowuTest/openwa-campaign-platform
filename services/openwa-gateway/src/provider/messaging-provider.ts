export type MessageType = 'text' | 'image' | 'video' | 'document';

export type SendRequest = {
  idempotencyKey: string;
  gatewayPoolId: string;
  sessionId: string;
  recipientMsisdn: string;
  messageType: MessageType;
  body?: string;
  mediaUrl?: string;
  clientReference?: string;
};

export type SendResult = {
  accepted: boolean;
  providerMessageId?: string;
  acceptedAt: string;
  errorCode?: string;
  errorDetail?: string;
  duplicate?: boolean;
};

export type SessionHealth = {
  ready: boolean;
  status: 'READY' | 'PAIRING' | 'DISCONNECTED' | 'PAUSED' | 'RESTRICTED' | 'UNKNOWN';
  checkedAt: string;
  detail?: string;
};

export type SessionRecord = {
  id?: string;
  name?: string;
  status?: string;
  phone?: string | null;
  pushName?: string | null;
  connectedAt?: string | null;
  lastActive?: string | null;
  createdAt?: string;
  updatedAt?: string;
  lastError?: string | null;
  engineLoaded?: boolean;
};

export interface MessagingProvider {
  send(request: SendRequest): Promise<SendResult>;
  health(sessionId: string): Promise<SessionHealth>;
  getSession?(sessionId: string): Promise<SessionRecord>;
  createSession?(name: string): Promise<SessionRecord>;
  startSession?(sessionId: string): Promise<SessionRecord>;
  stopSession?(sessionId: string): Promise<SessionRecord>;
  logoutSession?(sessionId: string): Promise<SessionRecord>;
  deleteSession?(sessionId: string): Promise<void>;
  qr?(sessionId: string): Promise<unknown>;
  pairingCode?(sessionId: string, phoneNumber: string): Promise<unknown>;
}
