import { Injectable, Logger, OnModuleDestroy, OnModuleInit } from '@nestjs/common';
import { GatewayIdentityService } from './gateway-identity.service';
import { GatewayMessagingService } from './gateway-messaging.service';
import { GatewayObservabilityService } from './observability.service';
import { RuntimeRegistrationService } from './runtime-registration.service';

type HeartbeatStatus =
  | 'PAIRING'
  | 'CONNECTING'
  | 'READY'
  | 'BUSY'
  | 'DRAINING'
  | 'PAUSED'
  | 'DISCONNECTED'
  | 'RECOVERING'
  | 'FAILED_RECOVERY'
  | 'RESTRICTED';

@Injectable()
export class SessionHeartbeatPublisherService implements OnModuleInit, OnModuleDestroy {
  private readonly logger = new Logger(SessionHeartbeatPublisherService.name);
  private readonly intervalMs = Math.max(1_000, Math.floor(senderHeartbeatTTLMilliseconds(process.env.SENDER_HEARTBEAT_TTL) / 3));
  private timer?: NodeJS.Timeout;
  private publishing = false;
  private shuttingDown = false;

  constructor(
    private readonly identity: GatewayIdentityService,
    private readonly gateway: GatewayMessagingService,
    private readonly runtime: RuntimeRegistrationService,
    private readonly observability: GatewayObservabilityService,
  ) {}

  async onModuleInit(): Promise<void> {
    await this.publishNow();
    this.timer = setInterval(() => void this.publishNow(), this.intervalMs);
    this.timer.unref();
  }

  onModuleDestroy(): void {
    this.shuttingDown = true;
    if (this.timer) clearInterval(this.timer);
    this.timer = undefined;
  }

  async publishNow(): Promise<void> {
    if (this.shuttingDown || this.publishing || !this.runtime.isRuntimeRegistered()) return;
    this.publishing = true;
    try {
      const observedAt = new Date();
      const usageDay = observedAt.toISOString().slice(0, 10);
      const sessions = await this.gateway.listSessions();
      for (const session of sessions) {
        const sessionId = String(session.id ?? '').trim();
        if (!sessionId) continue;
        try {
          const health = await this.gateway.health(sessionId);
          // Resample on the next scan rather than label yesterday's usage as today.
          if (new Date().toISOString().slice(0, 10) !== usageDay) break;
          const sentToday = Number(session.sentToday ?? 0);
          if (!Number.isSafeInteger(sentToday) || sentToday < 0) throw new Error('gateway session usage evidence is invalid');
          const report = {
            nodeId: this.identity.nodeId,
            sessionId,
            bootId: this.runtime.bootIdentity(),
            status: sessionHeartbeatStatus(health.status, health.pipeline),
            engineVersion: this.identity.adapterVersion,
            sentToday,
          };
          const accepted = await this.runtime.publishSessionHeartbeat(sessionId, report, observedAt);
          // The response is authoritative only for the UTC day of this sample.
          if (new Date().toISOString().slice(0, 10) === usageDay) {
            this.gateway.synchronizeSentToday(sessionId, accepted.sentToday);
          }
          this.observability.increment('openwa_gateway_session_heartbeat_total', { outcome: 'accepted' });
          this.observability.gauge('openwa_gateway_session_heartbeat_last_success_seconds', Math.floor(Date.now() / 1000), { sessionId });
        } catch (error) {
          this.observability.increment('openwa_gateway_session_heartbeat_total', { outcome: 'failed' });
          this.logger.warn(`sender-session heartbeat deferred for ${sessionId}: ${safeError(error)}`);
        }
      }
    } catch (error) {
      this.observability.increment('openwa_gateway_session_heartbeat_scan_total', { outcome: 'failed' });
      this.logger.warn(`sender-session heartbeat scan deferred: ${safeError(error)}`);
    } finally {
      this.publishing = false;
    }
  }
}

function sessionHeartbeatStatus(
  status: string,
  pipeline: { draining?: boolean; starting?: boolean; tornDown?: boolean },
): HeartbeatStatus {
  if (pipeline?.tornDown) return 'DISCONNECTED';
  if (pipeline?.draining) return 'DRAINING';
  if (pipeline?.starting) return 'CONNECTING';
  const normalized = String(status ?? '').trim().toUpperCase();
  switch (normalized) {
    case 'READY':
    case 'PAIRING':
    case 'DRAINING':
    case 'DISCONNECTED':
    case 'PAUSED':
    case 'RESTRICTED':
      return normalized;
    default:
      return 'RESTRICTED';
  }
}

function senderHeartbeatTTLMilliseconds(raw: string | undefined): number {
  const value = String(raw ?? '').trim();
  const milliseconds = value ? parseGoDurationMilliseconds(value) : 90_000;
  if (!Number.isFinite(milliseconds) || milliseconds < 10_000 || milliseconds > 600_000) {
    throw new Error('SENDER_HEARTBEAT_TTL must be between 10 seconds and 10 minutes');
  }
  return milliseconds;
}

function parseGoDurationMilliseconds(value: string): number {
  const pattern = /(\d+(?:\.\d+)?)(ms|s|m|h)/gu;
  let total = 0;
  let consumed = '';
  for (const match of value.matchAll(pattern)) {
    consumed += match[0];
    const amount = Number(match[1]);
    const multiplier = match[2] === 'ms' ? 1 : match[2] === 's' ? 1_000 : match[2] === 'm' ? 60_000 : 3_600_000;
    total += amount * multiplier;
  }
  return consumed === value && total > 0 ? total : Number.NaN;
}

function safeError(value: unknown): string {
  return (value instanceof Error ? value.message : String(value ?? 'unknown error'))
    .replace(/[\r\n\t]+/gu, ' ')
    .slice(0, 300);
}
