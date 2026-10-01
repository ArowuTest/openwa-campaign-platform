import {
  BadRequestException,
  ConflictException,
  Injectable,
  Logger,
  NotFoundException,
  OnModuleDestroy,
  OnModuleInit,
  ServiceUnavailableException,
} from '@nestjs/common';
import { createHash } from 'node:crypto';
import { mkdir, readdir, readFile, rename, rm, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { GatewayIdentityService } from '../gateway-identity.service';
import { RuntimeRegistrationService } from '../runtime-registration.service';
import { InboundMessagePublisherService } from '../inbound-message-publisher.service';
import { GatewayObservabilityService } from '../observability.service';
import { ProviderEventPublisherService } from '../provider-event-publisher.service';
import { BaileysAdapter } from '../retained-openwa/engine/adapters/baileys.adapter';
import { WhatsAppWebJsAdapter } from '../retained-openwa/engine/adapters/whatsapp-web-js.adapter';
import { EngineStatus } from '../retained-openwa/engine/interfaces/whatsapp-engine.interface';
import type { DeliveryStatus, EngineEventCallbacks, IncomingMessage, IWhatsAppEngine, MediaInput } from '../retained-openwa/engine/interfaces/whatsapp-engine.interface';
import type { LidMappingStore } from '../retained-openwa/engine/identity/lid-mapping-store';
import type { SendRequest, SendResult, SessionHealth, SessionProxyConfiguration, SessionRecord, SessionStartOptions, SessionTransportRuntimeConfiguration } from './messaging-provider';

type EngineName = 'WHATSAPP_WEB_JS' | 'BAILEYS';
type PersistentSession = {
  schemaVersion: 1;
  id: string;
  name: string;
  engine: EngineName;
  createdAt: string;
};
type RuntimeSession = {
  persistent: PersistentSession;
  engine?: IWhatsAppEngine;
  proxy?: SessionProxyConfiguration;
  runtime?: SessionTransportRuntimeConfiguration;
  generation: number;
  status: EngineStatus;
  qr?: string | null;
  phone?: string | null;
  pushName?: string | null;
  connectedAt?: string | null;
  updatedAt: string;
  lastError?: string | null;
  sentToday: number;
  sentTodayDate: string;
  reconnectAttempts: number;
  reconnectLastAttemptAt?: number;
  stuckAuthRecoveryUsed: boolean;
  reconnectTimer?: NodeJS.Timeout;
  ownershipRetirementPending?: boolean;
};

class BoundedLidMappingStore implements LidMappingStore {
  private readonly lidToPhone = new Map<string, string | null>();
  private readonly phoneToLids = new Map<string, Set<string>>();
  constructor(private readonly capacity = 5000) {}
  getCached(lid: string): string | null | undefined {
    if (!this.lidToPhone.has(lid)) return undefined;
    const phone = this.lidToPhone.get(lid) ?? null;
    this.lidToPhone.delete(lid);
    this.lidToPhone.set(lid, phone);
    return phone;
  }

  lidsForPhone(phone: string): string[] {
    return [...(this.phoneToLids.get(phone) ?? new Set<string>())];
  }

  async remember(lid: string, phone: string | null): Promise<void> {
    if (!lid) return;
    const previous = this.lidToPhone.get(lid);
    if (previous && previous !== phone) this.phoneToLids.get(previous)?.delete(lid);
    this.lidToPhone.delete(lid);
    this.lidToPhone.set(lid, phone);
    if (phone) {
      const lids = this.phoneToLids.get(phone) ?? new Set<string>();
      lids.add(lid);
      this.phoneToLids.set(phone, lids);
    }
    while (this.lidToPhone.size > this.capacity) {
      const oldest = this.lidToPhone.keys().next().value as string | undefined;
      if (!oldest) break;
      const oldPhone = this.lidToPhone.get(oldest);
      this.lidToPhone.delete(oldest);
      if (oldPhone) this.phoneToLids.get(oldPhone)?.delete(oldest);
    }
  }
}

@Injectable()
export class EmbeddedOpenWAEngineService implements OnModuleInit, OnModuleDestroy {
  private readonly logger = new Logger(EmbeddedOpenWAEngineService.name);
  private readonly sessions = new Map<string, RuntimeSession>();
  private readonly transitions = new Set<string>();
  private readonly pendingTeardowns = new Map<string, Promise<void>>();
  private readonly outboundReferences = new Map<string, string>();
  private readonly livenessFailures = new Map<string, number>();
  private watchdogTimer?: NodeJS.Timeout;
  private removeOwnershipListener?: () => void;
  private watchdogRunning = false;
  private readonly lidMappings = new BoundedLidMappingStore(
    boundedInteger(process.env.OPENWA_LID_CACHE_MAX, 5000, 100, 100_000),
  );
  private readonly registryDirectory = String(
    process.env.OPENWA_SESSION_REGISTRY_DIR ?? '/data/openwa-session-registry',
  );
  private readonly authDirectory = String(
    process.env.OPENWA_SESSION_DATA_DIR ?? '/app/session-data',
  );
  private readonly deploymentRuntime: SessionTransportRuntimeConfiguration;

  constructor(
    private readonly identity: GatewayIdentityService,
    private readonly publisher: ProviderEventPublisherService,
    private readonly inbound: InboundMessagePublisherService,
    private readonly observability: GatewayObservabilityService,
    private readonly runtimeRegistration: RuntimeRegistrationService,
  ) {
    this.deploymentRuntime = deploymentTransportRuntimeConfiguration(this.identity.engine);
  }

  async onModuleInit(): Promise<void> {
    this.removeOwnershipListener = this.runtimeRegistration.onSessionOwnershipLost(id => this.retireUnownedSession(id));
    await mkdir(this.registryDirectory, { recursive: true, mode: 0o700 });
    await mkdir(join(this.authDirectory, 'whatsapp-web-js'), { recursive: true, mode: 0o700 });
    await mkdir(join(this.authDirectory, 'baileys'), { recursive: true, mode: 0o700 });
    const files = (await readdir(this.registryDirectory)).filter(name => name.endsWith('.json')).sort();
    for (const file of files) {
      const persistent = validatePersistentSession(
        JSON.parse(await readFile(join(this.registryDirectory, file), 'utf8')),
      );
      if (this.sessions.has(persistent.id)) throw new Error(`duplicate embedded OpenWA session ${persistent.id}`);
      this.sessions.set(persistent.id, {
        persistent,
        generation: 0,
        status: EngineStatus.DISCONNECTED,
        updatedAt: new Date().toISOString(),
        sentToday: 0,
        sentTodayDate: utcDateKey(new Date()),
        reconnectAttempts: 0,
        stuckAuthRecoveryUsed: false,
      });
    }
    this.observability.gauge('openwa_gateway_embedded_sessions', this.sessions.size);
    this.startWatchdog();
  }

  async onModuleDestroy(): Promise<void> {
    this.removeOwnershipListener?.();
    this.removeOwnershipListener = undefined;
    if (this.watchdogTimer) clearInterval(this.watchdogTimer);
    this.watchdogTimer = undefined;
    this.livenessFailures.clear();
    for (const state of this.sessions.values()) {
      if (state.reconnectTimer) clearTimeout(state.reconnectTimer);
      const engine = state.engine;
      state.engine = undefined;
      state.generation += 1;
      if (engine) await withTimeout(engine.disconnect(), this.runtimePolicy(state).engineTeardownTimeoutMs).catch(() => undefined);
    }
  }

  async createSession(name: string): Promise<SessionRecord> {
    validateSessionId(name);
    if (this.sessions.has(name)) throw new ConflictException('session already exists on this worker');
    const persistent: PersistentSession = {
      schemaVersion: 1,
      id: name,
      name,
      engine: this.identity.engine,
      createdAt: new Date().toISOString(),
    };
    await this.persist(persistent);
    const state: RuntimeSession = {
      persistent,
      generation: 0,
      status: EngineStatus.DISCONNECTED,
      updatedAt: persistent.createdAt,
      sentToday: 0,
      sentTodayDate: utcDateKey(new Date()),
      reconnectAttempts: 0,
      stuckAuthRecoveryUsed: false,
    };
    this.sessions.set(name, state);
    this.observability.gauge('openwa_gateway_embedded_sessions', this.sessions.size);
    return this.record(state);
  }

  async getSession(sessionId: string): Promise<SessionRecord> {
    return this.record(this.requireSession(sessionId));
  }

  async listSessions(): Promise<SessionRecord[]> {
    return [...this.sessions.values()].map(state => this.record(state)).sort((left, right) => String(left.id ?? '').localeCompare(String(right.id ?? '')));
  }

  synchronizeSentToday(sessionId: string, sentToday: number): void {
    if (!Number.isSafeInteger(sentToday) || sentToday < 0) throw new TypeError('sentToday must be a non-negative safe integer');
    const state = this.requireSession(sessionId);
    const today = utcDateKey(new Date());
    if (state.sentTodayDate !== today) {
      state.sentTodayDate = today;
      state.sentToday = sentToday;
      return;
    }
    state.sentToday = Math.max(state.sentToday, sentToday);
  }

  async health(sessionId: string): Promise<SessionHealth> {
    const state = this.requireSession(sessionId);
    // Losing a lease is not a provider restriction. Keep the session non-sending
    // while teardown is pending without persisting sticky RESTRICTED telemetry.
    if (state.ownershipRetirementPending) {
      return { ready: false, status: 'DRAINING', checkedAt: new Date().toISOString(),
        detail: state.lastError ? safeError(state.lastError) : undefined,
        runtimeConfiguration: { ...this.runtimePolicy(state) } };
    }
    const status = state.engine?.getStatus() ?? state.status;
    return { ...mapHealth(status, state.lastError), runtimeConfiguration: { ...this.runtimePolicy(state) } };
  }
  async startSession(sessionId: string, options?: SessionStartOptions): Promise<SessionRecord> {
    this.runtimeRegistration.assertSessionOwned(sessionId);
    return this.withTransition(sessionId, async () => {
      await this.assertNoPendingTeardown(sessionId);
      const state = this.requireSession(sessionId);
      const requestedProxy = normaliseProxyConfiguration(options?.proxy);
      const requestedRuntime = options?.runtime === undefined
        ? this.deploymentRuntime
        : normaliseGovernedRuntimeConfiguration(options.runtime, sessionId, this.identity.gatewayPoolId);
      if (state.persistent.engine !== this.identity.engine) {
        throw new ConflictException('session engine does not match this gateway node');
      }
      if (state.engine) {
        if (!sameProxyConfiguration(state.proxy, requestedProxy)) {
          throw new ConflictException('session proxy cannot change while the engine is active');
        }
        if (!sameRuntimeConfiguration(this.runtimePolicy(state), requestedRuntime)) {
          throw new ConflictException('session transport runtime cannot change while the engine is active');
        }
        return this.record(state);
      }
      state.proxy = requestedProxy;
      state.runtime = { ...requestedRuntime };
      if (state.reconnectTimer) {
        clearTimeout(state.reconnectTimer);
        state.reconnectTimer = undefined;
      }
      state.reconnectAttempts = 0;
      state.stuckAuthRecoveryUsed = false;
      const maximum = boundedInteger(process.env.OPENWA_MAX_CONCURRENT_SESSIONS, 0, 0, 1000);
      const active = [...this.sessions.values()].filter(item => item.engine && item.persistent.id !== sessionId).length;
      if (maximum > 0 && active >= maximum) {
        throw new ConflictException(`maximum concurrent sessions reached (${maximum})`);
      }
      await this.initializeEngine(state);
      return this.record(state);
    });
  }

  async stopSession(sessionId: string): Promise<SessionRecord> {
    return this.withTransition(sessionId, async () => {
      const state = this.requireSession(sessionId);
      const runtime = this.runtimePolicy(state);
      this.cancelReconnect(state);
      const engine = this.detachEngine(state);
      state.proxy = undefined;
      state.runtime = undefined;
      if (engine) {
        try {
          await withTimeout(engine.disconnect(), runtime.engineTeardownTimeoutMs);
        } catch (error) {
          await withTimeout(engine.forceDestroy(), runtime.engineTeardownTimeoutMs).catch(() => undefined);
          state.lastError = safeError(error);
        }
      }
      state.status = EngineStatus.DISCONNECTED;
      state.qr = null;
      state.updatedAt = new Date().toISOString();
      return this.record(state);
    });
  }

  async logoutSession(sessionId: string): Promise<SessionRecord> {
    return this.withTransition(sessionId, async () => {
      const state = this.requireSession(sessionId);
      const runtime = this.runtimePolicy(state);
      this.cancelReconnect(state);
      const engine = this.detachEngine(state);
      state.proxy = undefined;
      state.runtime = undefined;
      if (!engine) throw new BadRequestException('session is not started');
      const operation = engine.logout();
      this.trackTeardown(sessionId, operation);
      try {
        await withTimeout(operation, runtime.engineTeardownTimeoutMs);
      } catch (error) {
        state.status = EngineStatus.DISCONNECTED;
        state.phone = null;
        state.pushName = null;
        state.lastError = safeError(error);
        state.updatedAt = new Date().toISOString();
        throw new ServiceUnavailableException('session logout is still completing; retry after teardown settles');
      }
      state.status = EngineStatus.DISCONNECTED;
      state.phone = null;
      state.pushName = null;
      state.qr = null;
      state.updatedAt = new Date().toISOString();
      return this.record(state);
    });
  }
  async deleteSession(sessionId: string): Promise<void> {
    await this.withTransition(sessionId, async () => {
      await this.assertNoPendingTeardown(sessionId);
      const state = this.requireSession(sessionId);
      const runtime = this.runtimePolicy(state);
      this.cancelReconnect(state);
      const engine = this.detachEngine(state);
      state.proxy = undefined;
      state.runtime = undefined;
      if (engine) {
        try {
          await withTimeout(engine.destroy(), runtime.engineTeardownTimeoutMs);
        } catch {
          await withTimeout(engine.forceDestroy(), runtime.engineTeardownTimeoutMs).catch(() => undefined);
        }
      }
      await Promise.all([
        rm(join(this.authDirectory, 'whatsapp-web-js', `session-${sessionId}`), { recursive: true, force: true }),
        rm(join(this.authDirectory, 'baileys', sessionId), { recursive: true, force: true }),
        rm(this.registryPath(sessionId), { force: true }),
      ]);
      this.sessions.delete(sessionId);
      this.observability.gauge('openwa_gateway_embedded_sessions', this.sessions.size);
    });
  }

  async qr(sessionId: string): Promise<{ sessionId: string; qr: string | null; status: string }> {
    const state = this.requireSession(sessionId);
    if (!state.engine) throw new BadRequestException('session is not started');
    return { sessionId, qr: state.engine.getQRCode() ?? state.qr ?? null, status: state.status };
  }
  async pairingCode(sessionId: string, phoneNumber: string): Promise<{ sessionId: string; pairingCode: string }> {
    const state = this.requireSession(sessionId);
    if (!state.engine) throw new BadRequestException('session is not started');
    this.runtimeRegistration.assertSessionOwned(sessionId);
    const pairingCode = await state.engine.requestPairingCode(phoneNumber);
    return { sessionId, pairingCode };
  }

  async send(request: SendRequest): Promise<SendResult> {
    this.runtimeRegistration.assertSessionOwned(request.sessionId, request.sessionLeaseVersion);
    const state = this.requireSession(request.sessionId);
    const engine = state.engine;
    if (!engine || engine.getStatus() !== EngineStatus.READY) {
      throw new ServiceUnavailableException('session engine is not ready');
    }
    if (request.engine !== state.persistent.engine || request.engine !== this.identity.engine) {
      throw new ConflictException('send engine does not match the embedded session engine');
    }
    const chatId = toChatId(request.recipientMsisdn);
    const result = await this.sendThroughEngine(engine, chatId, request);
    if (!result.id?.trim()) {
      throw new ServiceUnavailableException('OpenWA engine returned no provider message identifier');
    }
    const acceptedAt = timestampToISO(result.timestamp);
    this.recordAcceptedSend(state, acceptedAt);
    if (request.clientReference) this.rememberOutboundReference(result.id, request.clientReference);
    await this.queueProviderEvent(request.sessionId, result.id, 'sent', request.clientReference, acceptedAt);
    return { accepted: true, providerMessageId: result.id, acceptedAt };
  }
  private async sendThroughEngine(engine: IWhatsAppEngine, chatId: string, request: SendRequest) {
    if (request.messageType === 'text') {
      if (!request.body?.trim()) throw new BadRequestException('text message body is required');
      return engine.sendTextMessage(chatId, request.body);
    }
    if (!request.mediaUrl?.trim()) throw new BadRequestException('mediaUrl is required for media sends');
    const media: MediaInput = {
      mimetype: 'application/octet-stream',
      data: request.mediaUrl,
      caption: request.body?.trim() || undefined,
      filename: request.messageType === 'document' ? safeFilename(request.mediaUrl) : undefined,
    };
    if (request.messageType === 'image') return engine.sendImageMessage(chatId, media);
    if (request.messageType === 'video') return engine.sendVideoMessage(chatId, media);
    return engine.sendDocumentMessage(chatId, media);
  }

  private async initializeEngine(state: RuntimeSession, reconnect = false): Promise<void> {
    this.runtimeRegistration.assertSessionOwned(state.persistent.id);
    const generation = state.generation + 1;
    state.generation = generation;
    state.status = EngineStatus.INITIALIZING;
    state.lastError = null;
    state.qr = null;
    state.updatedAt = new Date().toISOString();
    const engine = this.createEngine(state.persistent);
    state.engine = engine;
    try {
      await engine.initialize(this.callbacks(state.persistent.id, generation, engine));
      this.runtimeRegistration.assertSessionOwned(state.persistent.id);
      if (this.isCurrent(state.persistent.id, generation, engine)) {
        state.status = engine.getStatus();
        state.phone = engine.getPhoneNumber();
        state.pushName = engine.getPushName();
        state.updatedAt = new Date().toISOString();
      }
      this.observability.increment('openwa_gateway_engine_start_total', {
        engine: state.persistent.engine,
        outcome: 'accepted',
      });
    } catch (error) {
      if (this.isCurrent(state.persistent.id, generation, engine)) {
        const callbackMarkedTerminal = state.status === EngineStatus.FAILED || state.status === EngineStatus.ACTION_REQUIRED;
        state.engine = undefined;
        state.status = reconnect && !callbackMarkedTerminal ? EngineStatus.DISCONNECTED : EngineStatus.FAILED;
        state.lastError = safeError(error);
        state.updatedAt = new Date().toISOString();
      }
      await withTimeout(engine.forceDestroy(), this.runtimePolicy(state).engineTeardownTimeoutMs).catch(() => undefined);
      this.observability.increment('openwa_gateway_engine_start_total', {
        engine: state.persistent.engine,
        outcome: 'failed',
      });
      throw new ServiceUnavailableException(`session engine failed to start: ${safeError(error)}`);
    }
  }
  private createEngine(session: PersistentSession): IWhatsAppEngine {
    const state = this.sessions.get(session.id);
    const proxy = state?.proxy;
    if (session.engine === 'BAILEYS') {
      const runtime = this.runtimePolicy(state!);
      const deploymentBaseDelay = process.env.OPENWA_RECONNECT_BASE_DELAY_MS?.trim();
      return new BaileysAdapter({
        sessionId: session.name,
        dbSessionId: session.id,
        assertSessionOwnership: () => this.runtimeRegistration.assertSessionOwned(session.id),
        authDir: join(this.authDirectory, 'baileys'),
        proxyUrl: proxy?.url,
        proxyType: proxy?.type,
        reconnectMode: runtime.reconnectMode,
        reconnectMaxAttempts: runtime.reconnectMaxAttempts,
        reconnectBaseDelayMs: runtime.source === 'GOVERNED_CONFIGURATION' || deploymentBaseDelay ? runtime.reconnectBaseDelayMs : undefined,
        reconnectStabilityResetMs: runtime.reconnectStabilityResetMs,
        lidMappingStore: this.lidMappings,
      });
    }
    const args = parsePuppeteerArgs(process.env.PUPPETEER_ARGS);
    return new WhatsAppWebJsAdapter({
      sessionId: session.name,
      sessionDataPath: join(this.authDirectory, 'whatsapp-web-js'),
      puppeteer: {
        headless: process.env.PUPPETEER_HEADLESS !== 'false',
        args,
        executablePath: process.env.PUPPETEER_EXECUTABLE_PATH || undefined,
      },
      proxy: proxy ? { url: proxy.url, type: proxy.type } : undefined,
      lidMappingStore: this.lidMappings,
    });
  }

  private callbacks(sessionId: string, generation: number, engine: IWhatsAppEngine): EngineEventCallbacks {
    const current = () => this.isCurrent(sessionId, generation, engine);
    return {
      onQRCode: qr => {
        if (!current()) return;
        const state = this.sessions.get(sessionId)!;
        state.qr = qr;
        state.status = EngineStatus.QR_READY;
        state.updatedAt = new Date().toISOString();
      },
      onReady: (phone, pushName) => {
        if (!current()) return;
        const state = this.sessions.get(sessionId)!;
        state.status = EngineStatus.READY;
        state.phone = phone || null;
        state.pushName = pushName || null;
        state.connectedAt = new Date().toISOString();
        state.updatedAt = state.connectedAt;
        state.lastError = null;
        state.reconnectAttempts = 0;
      },
      onStateChanged: status => {
        if (!current()) return;
        const state = this.sessions.get(sessionId)!;
        state.status = status;
        state.updatedAt = new Date().toISOString();
      },
      onMessageAck: (messageId, status) => {
        if (!current()) return;
        void this.publishAck(sessionId, messageId, status).catch(error => this.logCallbackError('ack', error));
      },
      onMessage: message => {
        if (!current() || message.fromMe) return;
        void this.publishInbound(sessionId, message).catch(error => this.logCallbackError('inbound', error));
      },
      onDisconnected: reason => {
        if (!current()) return;
        this.handleDisconnected(sessionId, generation, engine, reason);
      },
      onActionRequired: reason => {
        if (!current()) return;
        const state = this.sessions.get(sessionId)!;
        state.status = EngineStatus.ACTION_REQUIRED;
        state.lastError = safeError(reason);
        state.updatedAt = new Date().toISOString();
        this.cancelReconnect(state);
      },
      onError: reason => {
        if (!current()) return;
        const state = this.sessions.get(sessionId)!;
        state.status = EngineStatus.FAILED;
        state.lastError = safeError(reason);
        state.updatedAt = new Date().toISOString();
        this.cancelReconnect(state);
      },
      onCredentialTeardownStarted: operation => this.trackTeardown(sessionId, operation),
      claimStuckAuthRecovery: () => {
        if (!current()) return false;
        const state = this.sessions.get(sessionId)!;
        if (state.stuckAuthRecoveryUsed) return false;
        state.stuckAuthRecoveryUsed = true;
        return true;
      },
    };
  }
  private startWatchdog(): void {
    if (this.watchdogTimer) return;
    this.watchdogTimer = setInterval(() => {
      void this.watchdogTick().catch(error => this.logCallbackError('watchdog', error));
    }, watchdogIntervalMs());
    this.watchdogTimer.unref?.();
  }

  private async watchdogTick(): Promise<void> {
    if (this.watchdogRunning) return;
    this.watchdogRunning = true;
    try {
      await Promise.allSettled(
        [...this.sessions.entries()].map(([sessionId, state]) => this.probeSessionLiveness(sessionId, state)),
      );
    } finally {
      this.watchdogRunning = false;
    }
  }

  private async probeSessionLiveness(sessionId: string, state: RuntimeSession): Promise<void> {
    if (this.transitions.has(sessionId)) return;
    const engine = state.engine;
    if (!engine || typeof engine.probeLiveness !== 'function') {
      this.livenessFailures.delete(sessionId);
      return;
    }
    const status = engine.getStatus();
    const observeOnly = status === EngineStatus.ACTION_REQUIRED;
    if (status !== EngineStatus.READY && !observeOnly) {
      this.livenessFailures.delete(sessionId);
      return;
    }
    const generation = state.generation;
    const runtime = this.runtimePolicy(state);
    let alive = false;
    try {
      alive = await withTimeout(engine.probeLiveness(), runtime.watchdogProbeTimeoutMs);
    } catch {
      alive = false;
    }
    if (!this.isCurrent(sessionId, generation, engine) || this.transitions.has(sessionId)) return;
    if (alive) {
      this.livenessFailures.delete(sessionId);
      return;
    }
    const failures = (this.livenessFailures.get(sessionId) ?? 0) + 1;
    if (observeOnly) {
      this.livenessFailures.set(sessionId, failures);
      if (failures === 1) {
        this.logger.warn(`OpenWA session ${sessionId} failed a liveness probe while awaiting operator action`);
      }
      return;
    }
    if (failures < runtime.watchdogFailureThreshold) {
      this.livenessFailures.set(sessionId, failures);
      this.logger.warn(`OpenWA session ${sessionId} failed a liveness probe; waiting for confirmation`, {
        failures,
        threshold: runtime.watchdogFailureThreshold,
      });
      return;
    }
    this.livenessFailures.delete(sessionId);
    await this.recoverUnresponsiveEngine(sessionId, generation, engine);
  }

  private async recoverUnresponsiveEngine(
    sessionId: string,
    generation: number,
    engine: IWhatsAppEngine,
  ): Promise<void> {
    try {
      await this.withTransition(sessionId, async () => {
        if (!this.isCurrent(sessionId, generation, engine)) return;
        const state = this.requireSession(sessionId);
        this.cancelReconnect(state);
        this.detachEngine(state);
        state.status = EngineStatus.DISCONNECTED;
        state.lastError = 'liveness probe failed repeatedly';
        state.updatedAt = new Date().toISOString();
        await withTimeout(engine.forceDestroy(), this.runtimePolicy(state).engineTeardownTimeoutMs).catch(() => undefined);
        this.observability.increment('openwa_gateway_engine_watchdog_total', {
          engine: state.persistent.engine,
          outcome: 'recovered',
        });
        this.scheduleReconnect(state, true);
      });
    } catch (error) {
      if (error instanceof ConflictException) return;
      throw error;
    }
  }

  private handleDisconnected(sessionId: string, generation: number, engine: IWhatsAppEngine, reason: string): void {
    if (!this.isCurrent(sessionId, generation, engine)) return;
    const state = this.sessions.get(sessionId)!;
    state.status = EngineStatus.DISCONNECTED;
    state.lastError = safeError(reason);
    state.updatedAt = new Date().toISOString();
    this.observability.increment('openwa_gateway_engine_disconnect_total', { engine: state.persistent.engine });

    // Baileys owns transient reconnects inside its retained lifecycle delegate.
    if (state.persistent.engine === 'BAILEYS') return;

    state.engine = undefined;
    state.generation += 1;
    state.qr = null;
    void withTimeout(engine.forceDestroy(), this.runtimePolicy(state).engineTeardownTimeoutMs).catch(() => undefined);
    this.scheduleReconnect(state);
  }

  private scheduleReconnect(state: RuntimeSession, forced = false): void {
    if ((!forced && state.persistent.engine !== 'WHATSAPP_WEB_JS') || state.reconnectTimer) return;
    const runtime = this.runtimePolicy(state);
    const now = Date.now();
    if (state.reconnectLastAttemptAt && now - state.reconnectLastAttemptAt >= runtime.reconnectStabilityResetMs) {
      state.reconnectAttempts = 0;
    }
    const maxAttempts = runtime.reconnectMode === 'UNBOUNDED'
      ? Number.POSITIVE_INFINITY
      : runtime.reconnectMode === 'DISABLED'
        ? 0
        : runtime.reconnectMaxAttempts;
    if (state.reconnectAttempts >= maxAttempts) {
      state.status = EngineStatus.FAILED;
      state.lastError = maxAttempts === 0
        ? 'Auto-reconnect is disabled; restart the session manually.'
        : `Reconnection failed after ${state.reconnectAttempts} attempts; restart the session manually.`;
      return;
    }
    const baseDelay = runtime.reconnectBaseDelayMs;
    const rawDelay = baseDelay * Math.pow(2, state.reconnectAttempts) + Math.random() * 1000;
    const delayMs = Math.min(Number.isFinite(rawDelay) ? rawDelay : baseDelay, 3_600_000);
    state.reconnectAttempts += 1;
    state.reconnectLastAttemptAt = now;
    if (state.reconnectAttempts % 5 === 0) {
      this.logger.warn(`OpenWA session ${state.persistent.id} remains in a reconnect loop`, {
        attempt: state.reconnectAttempts,
      });
    }
    state.reconnectTimer = setTimeout(() => {
      state.reconnectTimer = undefined;
      void this.executeReconnect(state.persistent.id);
    }, delayMs);
    state.reconnectTimer.unref?.();
  }

  private async executeReconnect(sessionId: string): Promise<void> {
    const state = this.sessions.get(sessionId);
    if (!state || state.engine || state.status === EngineStatus.ACTION_REQUIRED || state.status === EngineStatus.FAILED) return;
    if (this.transitions.has(sessionId)) {
      this.scheduleReconnect(state);
      return;
    }
    try {
      await this.withTransition(sessionId, async () => {
        await this.assertNoPendingTeardown(sessionId);
        const current = this.requireSession(sessionId);
        if (current.engine) return;
        await this.initializeEngine(current, true);
      });
    } catch (error) {
      const current = this.sessions.get(sessionId);
      if (!current || current.status === EngineStatus.ACTION_REQUIRED || current.status === EngineStatus.FAILED) return;
      current.status = EngineStatus.DISCONNECTED;
      current.lastError = safeError(error);
      current.updatedAt = new Date().toISOString();
      this.scheduleReconnect(current, current.persistent.engine === 'BAILEYS');
    }
  }

  private async publishAck(sessionId: string, messageId: string, status: DeliveryStatus): Promise<void> {
    if (!messageId?.trim()) return;
    if (status === 'pending' || status === 'sent') {
      await this.queueProviderEvent(sessionId, messageId, 'sent', this.outboundReferences.get(messageId));
      return;
    }
    if (status === 'delivered') {
      await this.queueProviderEvent(sessionId, messageId, 'delivered', this.outboundReferences.get(messageId));
      return;
    }
    if (status === 'read') {
      await this.queueProviderEvent(sessionId, messageId, 'read', this.outboundReferences.get(messageId));
      this.outboundReferences.delete(messageId);
      return;
    }
    await this.queueProviderEvent(sessionId, messageId, 'failed', this.outboundReferences.get(messageId));
    this.outboundReferences.delete(messageId);
  }

  private async queueProviderEvent(
    sessionId: string,
    providerMessageId: string,
    status: 'sent' | 'delivered' | 'read' | 'failed',
    clientReference?: string,
    occurredAt = new Date().toISOString(),
  ): Promise<void> {
    const eventType = status === 'sent'
      ? 'message.sent'
      : status === 'delivered'
        ? 'message.delivered'
        : status === 'read'
          ? 'message.read'
          : 'message.failed_permanent';
    const event = this.publisher.create({
      eventId: stableEventId(sessionId, providerMessageId, status),
      eventType,
      sessionId,
      providerMessageId,
      clientReference,
      occurredAt,
      ...(status === 'failed'
        ? { errorCode: 'OPENWA_MESSAGE_FAILED', errorDetail: 'OpenWA reported a failed outbound message' }
        : {}),
    });
    await this.publisher.queue(event);
  }

  private async publishInbound(sessionId: string, message: IncomingMessage): Promise<void> {
    const body = String(message.body ?? '').trim();
    if (!body || body.length > 4096) return;
    const senderMsisdn = senderE164(message.senderPhone ?? message.author ?? message.from);
    const quotedProviderMessageId = cleanId(message.quotedMessage?.id);
    if (!senderMsisdn && !quotedProviderMessageId) {
      this.logger.warn('Dropped inbound message without safe correlation evidence', { sessionId });
      return;
    }
    const providerMessageId = cleanId(message.id);
    const eventId = stableEventId(sessionId, providerMessageId ?? `${message.timestamp}:${message.from}`, 'inbound');
    await this.inbound.queue({
      schemaVersion: '1.0',
      eventId,
      sessionId,
      providerMessageId,
      quotedProviderMessageId,
      senderMsisdn,
      messageText: body,
      occurredAt: timestampToISO(message.timestamp),
    });
  }

  private rememberOutboundReference(providerMessageId: string, clientReference: string): void {
    this.outboundReferences.delete(providerMessageId);
    this.outboundReferences.set(providerMessageId, clientReference);
    while (this.outboundReferences.size > 10_000) {
      const oldest = this.outboundReferences.keys().next().value as string | undefined;
      if (!oldest) break;
      this.outboundReferences.delete(oldest);
    }
  }

  private detachEngine(state: RuntimeSession): IWhatsAppEngine | undefined {
    const engine = state.engine;
    state.engine = undefined;
    state.generation += 1;
    state.qr = null;
    return engine;
  }

  private isCurrent(sessionId: string, generation: number, engine: IWhatsAppEngine): boolean {
    const state = this.sessions.get(sessionId);
    return Boolean(state && state.generation === generation && state.engine === engine);
  }

  private retireUnownedSession(sessionId: string): void {
    const state = this.sessions.get(sessionId);
    // Repeated loss notifications must not invalidate an outstanding teardown
    // or report DISCONNECTED before that same engine actually retires.
    if (!state || state.ownershipRetirementPending) return;
    this.cancelReconnect(state);
    const engine = this.detachEngine(state);
    state.status = EngineStatus.ACTION_REQUIRED;
    state.lastError = 'Current process session ownership expired or was rejected';
    state.updatedAt = new Date().toISOString();
    if (!engine) { state.status = EngineStatus.DISCONNECTED; return; }
    state.ownershipRetirementPending = true;
    // Detach synchronously before asynchronous teardown: callbacks and reconnects
    // from this retired instance cannot restore READY. Do not remove credentials.
    const cleanup = Promise.resolve().then(() => engine.forceDestroy()).then(() => {
      // The pending flag has a single owner: this teardown. A concurrent stop
      // can advance callback generation, but cannot make this cleanup obsolete.
      state.ownershipRetirementPending = false;
      if (!state.engine) {
        state.status = EngineStatus.DISCONNECTED;
        state.updatedAt = new Date().toISOString();
      }
    }).catch(error => {
      state.ownershipRetirementPending = false;
      if (!state.engine) {
        state.status = EngineStatus.ACTION_REQUIRED;
        state.lastError = safeError(error);
        // A manual stop cannot turn a genuine teardown failure into success.
      }
    });
    this.trackTeardown(sessionId, cleanup);
  }

  private cancelReconnect(state: RuntimeSession): void {
    if (state.reconnectTimer) clearTimeout(state.reconnectTimer);
    state.reconnectTimer = undefined;
  }

  private trackTeardown(sessionId: string, operation: Promise<void>): void {
    const tracked = Promise.resolve(operation).then(() => undefined, () => undefined);
    this.pendingTeardowns.set(sessionId, tracked);
    void tracked.finally(() => {
      if (this.pendingTeardowns.get(sessionId) === tracked) this.pendingTeardowns.delete(sessionId);
    });
  }

  private async assertNoPendingTeardown(sessionId: string): Promise<void> {
    const pending = this.pendingTeardowns.get(sessionId);
    if (!pending) return;
    let timer: NodeJS.Timeout | undefined;
    const settled = await Promise.race([
      pending.then(() => true),
      new Promise<boolean>(resolve => {
        timer = setTimeout(() => resolve(false), 250);
        timer.unref?.();
      }),
    ]);
    if (timer) clearTimeout(timer);
    if (!settled) throw new ConflictException('session credential teardown is still in progress');
  }

  private async withTransition<T>(sessionId: string, operation: () => Promise<T>): Promise<T> {
    validateSessionId(sessionId);
    if (this.transitions.has(sessionId)) {
      throw new ConflictException('session lifecycle transition is already in progress');
    }
    this.transitions.add(sessionId);
    try {
      return await operation();
    } finally {
      this.transitions.delete(sessionId);
    }
  }

  private async persist(session: PersistentSession): Promise<void> {
    const path = this.registryPath(session.id);
    const temporary = `${path}.${process.pid}.${Date.now()}.tmp`;
    await writeFile(temporary, JSON.stringify(session), { encoding: 'utf8', mode: 0o600, flag: 'wx' });
    await rename(temporary, path);
  }

  private registryPath(sessionId: string): string {
    return join(this.registryDirectory, `${Buffer.from(validateSessionId(sessionId)).toString('base64url')}.json`);
  }

  private requireSession(sessionId: string): RuntimeSession {
    validateSessionId(sessionId);
    const state = this.sessions.get(sessionId);
    if (!state) throw new NotFoundException(`session ${sessionId} is not registered on this worker`);
    return state;
  }

  private runtimePolicy(state: RuntimeSession): SessionTransportRuntimeConfiguration {
    return state.runtime ?? this.deploymentRuntime;
  }

  private recordAcceptedSend(state: RuntimeSession, acceptedAt: string): void {
    const date = utcDateKey(new Date(acceptedAt));
    // Late provider evidence retains its event timestamp, but cannot roll the
    // active usage counter backwards and erase newer-day accepted sends.
    if (date < state.sentTodayDate) return;
    if (state.sentTodayDate !== date) {
      state.sentTodayDate = date;
      state.sentToday = 0;
    }
    state.sentToday += 1;
  }

  private record(state: RuntimeSession): SessionRecord {
    // An idle session still crosses UTC midnight. Reset before publishing usage,
    // otherwise the control plane would seed the new day with yesterday's count.
    const today = utcDateKey(new Date());
    if (state.sentTodayDate !== today) {
      state.sentTodayDate = today;
      state.sentToday = 0;
    }
    return {
      id: state.persistent.id,
      name: state.persistent.name,
      status: state.status,
      sentToday: state.sentToday,
      phone: state.phone ?? null,
      pushName: state.pushName ?? null,
      connectedAt: state.connectedAt ?? null,
      lastActive: state.updatedAt,
      createdAt: state.persistent.createdAt,
      updatedAt: state.updatedAt,
      lastError: state.lastError ?? null,
      engineLoaded: Boolean(state.engine),
    };
  }
  private logCallbackError(kind: string, error: unknown): void {
    this.observability.increment('openwa_gateway_engine_callback_persistence_total', { kind, outcome: 'failed' });
    this.logger.warn(`embedded OpenWA ${kind} callback persistence deferred: ${safeError(error)}`);
  }
}

function validatePersistentSession(value: unknown): PersistentSession {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('invalid embedded OpenWA session registry entry');
  const record = value as Record<string, unknown>;
  const id = validateSessionId(String(record.id ?? ''));
  const name = validateSessionId(String(record.name ?? ''));
  const engine = String(record.engine ?? '') as EngineName;
  if (record.schemaVersion !== 1 || id !== name || !['WHATSAPP_WEB_JS', 'BAILEYS'].includes(engine)) {
    throw new Error(`invalid embedded OpenWA session registry entry for ${id}`);
  }
  const createdAt = String(record.createdAt ?? '');
  if (Number.isNaN(Date.parse(createdAt))) throw new Error(`invalid createdAt for embedded OpenWA session ${id}`);
  return { schemaVersion: 1, id, name, engine, createdAt: new Date(createdAt).toISOString() };
}

function validateSessionId(value: string): string {
  const id = String(value ?? '').trim();
  if (!/^[A-Za-z0-9_-]{3,50}$/.test(id)) throw new BadRequestException('session identifier format is invalid');
  return id;
}
function normaliseProxyConfiguration(value: SessionProxyConfiguration | undefined): SessionProxyConfiguration | undefined {
  if (value === undefined) return undefined;
  const type = String(value.type ?? '').trim().toLowerCase() as SessionProxyConfiguration['type'];
  if (!['http', 'https', 'socks4', 'socks5'].includes(type)) throw new BadRequestException('session proxy type is invalid');
  let parsed: URL;
  try {
    parsed = new URL(String(value.url ?? '').trim());
  } catch {
    throw new BadRequestException('session proxy URL is invalid');
  }
  if (!parsed.hostname || parsed.protocol !== `${type}:` || parsed.hash) throw new BadRequestException('session proxy URL does not match its type');
  return { url: String(value.url ?? '').trim(), type };
}

function sameProxyConfiguration(left: SessionProxyConfiguration | undefined, right: SessionProxyConfiguration | undefined): boolean {
  return left?.url === right?.url && left?.type === right?.type;
}

function deploymentTransportRuntimeConfiguration(engine: EngineName): SessionTransportRuntimeConfiguration {
  const rawAttempts = process.env.OPENWA_RECONNECT_MAX_ATTEMPTS?.trim();
  let reconnectMode: SessionTransportRuntimeConfiguration['reconnectMode'] = 'UNBOUNDED';
  let reconnectMaxAttempts = 0;
  if (rawAttempts !== undefined && rawAttempts !== '') {
    const parsed = strictInteger('OPENWA_RECONNECT_MAX_ATTEMPTS', rawAttempts, 0, 20);
    reconnectMode = parsed === 0 ? 'DISABLED' : 'BOUNDED';
    reconnectMaxAttempts = parsed;
  }
  return {
    reconnectMode,
    reconnectMaxAttempts,
    reconnectBaseDelayMs: strictEnvInteger('OPENWA_RECONNECT_BASE_DELAY_MS', engine === 'BAILEYS' ? 1000 : 5000, 1000, 300_000),
    reconnectStabilityResetMs: strictEnvInteger('OPENWA_RECONNECT_STABILITY_RESET_MS', 300_000, 60_000, 3_600_000),
    watchdogProbeTimeoutMs: strictEnvInteger('OPENWA_WATCHDOG_PROBE_TIMEOUT_MS', 15_000, 100, 60_000),
    watchdogFailureThreshold: strictEnvInteger('OPENWA_WATCHDOG_FAILURE_THRESHOLD', 2, 1, 10),
    engineTeardownTimeoutMs: strictEnvInteger('OPENWA_ENGINE_TEARDOWN_TIMEOUT_MS', 30_000, 1000, 120_000),
    source: 'DEPLOYMENT_BOOTSTRAP',
  };
}

function mapHealth(status: EngineStatus, detail?: string | null): SessionHealth {
  let mapped: SessionHealth['status'] = 'UNKNOWN';
  if (status === EngineStatus.READY) mapped = 'READY';
  else if ([EngineStatus.INITIALIZING, EngineStatus.QR_READY, EngineStatus.AUTHENTICATING].includes(status)) mapped = 'PAIRING';
  else if (status === EngineStatus.DISCONNECTED) mapped = 'DISCONNECTED';
  else if ([EngineStatus.ACTION_REQUIRED, EngineStatus.FAILED].includes(status)) mapped = 'RESTRICTED';
  return {
    ready: mapped === 'READY',
    status: mapped,
    checkedAt: new Date().toISOString(),
    detail: detail ? safeError(detail) : undefined,
  };
}

function parsePuppeteerArgs(raw: string | undefined): string[] {
  const value = raw?.trim() || '--no-sandbox,--disable-setuid-sandbox,--disable-dev-shm-usage,--disable-gpu';
  const args = value.split(/[\s,]+/).filter(Boolean);
  if (!args.some(arg => arg.startsWith('--lang='))) args.push('--lang=en-US');
  return args;
}

function normaliseGovernedRuntimeConfiguration(
  value: SessionTransportRuntimeConfiguration,
  sessionId: string,
  gatewayPoolId: string,
): SessionTransportRuntimeConfiguration {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new BadRequestException('session transport runtime configuration is invalid');
  if (value.source !== 'GOVERNED_CONFIGURATION') throw new BadRequestException('session transport runtime source must be governed configuration');
  const configurationId = String(value.configurationId ?? '').trim();
  if (!configurationId) throw new BadRequestException('session transport runtime configuration evidence is missing');
  const version = strictInteger('session transport runtime version', value.version, 1, Number.MAX_SAFE_INTEGER);
  const scopeType = String(value.scopeType ?? '').trim().toUpperCase() as NonNullable<SessionTransportRuntimeConfiguration['scopeType']>;
  const scopeId = String(value.scopeId ?? '').trim();
  if (!['PLATFORM', 'GATEWAY_POOL', 'SENDER_POOL', 'SENDER_SESSION'].includes(scopeType)) {
    throw new BadRequestException('session transport runtime scope is invalid');
  }
  if (scopeType === 'PLATFORM' && scopeId) throw new BadRequestException('platform transport runtime scope must not include scopeId');
  if (scopeType === 'GATEWAY_POOL' && scopeId !== gatewayPoolId) throw new BadRequestException('gateway-pool transport runtime scope does not match this worker');
  if (scopeType === 'SENDER_POOL' && !scopeId) throw new BadRequestException('sender-pool transport runtime scope requires scopeId');
  if (scopeType === 'SENDER_SESSION' && scopeId !== sessionId) throw new BadRequestException('sender-session transport runtime scope does not match the requested session');

  const reconnectMode = String(value.reconnectMode ?? '').trim().toUpperCase() as SessionTransportRuntimeConfiguration['reconnectMode'];
  const reconnectMaxAttempts = strictInteger('reconnectMaxAttempts', value.reconnectMaxAttempts, 0, 20);
  if (!['UNBOUNDED', 'DISABLED', 'BOUNDED'].includes(reconnectMode)) throw new BadRequestException('reconnectMode is invalid');
  if (reconnectMode === 'BOUNDED' && reconnectMaxAttempts < 1) throw new BadRequestException('bounded reconnect mode requires at least one attempt');
  if (reconnectMode !== 'BOUNDED' && reconnectMaxAttempts !== 0) throw new BadRequestException('unbounded or disabled reconnect mode requires reconnectMaxAttempts=0');

  return {
    reconnectMode,
    reconnectMaxAttempts,
    reconnectBaseDelayMs: strictInteger('reconnectBaseDelayMs', value.reconnectBaseDelayMs, 1000, 300_000),
    reconnectStabilityResetMs: strictInteger('reconnectStabilityResetMs', value.reconnectStabilityResetMs, 60_000, 3_600_000),
    watchdogProbeTimeoutMs: strictInteger('watchdogProbeTimeoutMs', value.watchdogProbeTimeoutMs, 100, 60_000),
    watchdogFailureThreshold: strictInteger('watchdogFailureThreshold', value.watchdogFailureThreshold, 1, 10),
    engineTeardownTimeoutMs: strictInteger('engineTeardownTimeoutMs', value.engineTeardownTimeoutMs, 1000, 120_000),
    source: 'GOVERNED_CONFIGURATION',
    configurationId,
    scopeType,
    scopeId: scopeId || undefined,
    version,
  };
}

function sameRuntimeConfiguration(left: SessionTransportRuntimeConfiguration, right: SessionTransportRuntimeConfiguration): boolean {
  return left.reconnectMode === right.reconnectMode &&
    left.reconnectMaxAttempts === right.reconnectMaxAttempts &&
    left.reconnectBaseDelayMs === right.reconnectBaseDelayMs &&
    left.reconnectStabilityResetMs === right.reconnectStabilityResetMs &&
    left.watchdogProbeTimeoutMs === right.watchdogProbeTimeoutMs &&
    left.watchdogFailureThreshold === right.watchdogFailureThreshold &&
    left.engineTeardownTimeoutMs === right.engineTeardownTimeoutMs &&
    left.source === right.source &&
    left.configurationId === right.configurationId &&
    left.scopeType === right.scopeType &&
    left.scopeId === right.scopeId &&
    left.version === right.version;
}

function watchdogIntervalMs(): number {
  return strictEnvInteger('OPENWA_WATCHDOG_INTERVAL_MS', 60_000, 1_000, 300_000);
}

async function withTimeout<T>(operation: Promise<T>, timeoutMs: number): Promise<T> {
  let timer: NodeJS.Timeout | undefined;
  try {
    return await Promise.race([
      operation,
      new Promise<T>((_resolve, reject) => {
        timer = setTimeout(() => reject(new Error(`operation timed out after ${timeoutMs}ms`)), timeoutMs);
        timer.unref?.();
      }),
    ]);
  } finally {
    if (timer) clearTimeout(timer);
  }
}

function stableEventId(sessionId: string, providerMessageId: string, kind: string): string {
  const digest = createHash('sha256').update(sessionId).update('\n').update(providerMessageId).update('\n').update(kind).digest('base64url');
  return `openwa:${kind}:${digest}`;
}

function toChatId(e164: string): string {
  const digits = String(e164 ?? '').replace(/\D/g, '');
  if (!/^[1-9]\d{7,14}$/.test(digits)) throw new BadRequestException('recipient MSISDN is invalid');
  return `${digits}@c.us`;
}
function timestampToISO(value: number | undefined): string {
  if (typeof value === 'number' && Number.isFinite(value) && value > 0) return new Date(value * 1000).toISOString();
  return new Date().toISOString();
}

function utcDateKey(value: Date): string {
  if (Number.isNaN(value.getTime())) throw new TypeError('heartbeat usage timestamp is invalid');
  return value.toISOString().slice(0, 10);
}

function safeFilename(value: string | undefined): string {
  try {
    const pathname = new URL(value ?? '').pathname;
    const name = pathname.split('/').pop() || 'document';
    return name.replace(/[^A-Za-z0-9._-]/g, '_').slice(0, 120) || 'document';
  } catch {
    return 'document';
  }
}

function senderE164(value: unknown): string | undefined {
  let text = String(value ?? '').trim();
  if (!text) return undefined;
  if (text.startsWith('+')) return /^\+[1-9]\d{7,14}$/.test(text) ? text : undefined;
  text = text.split('@')[0].replace(/\D/g, '');
  return /^[1-9]\d{7,14}$/.test(text) ? `+${text}` : undefined;
}

function cleanId(value: unknown): string | undefined {
  const text = String(value ?? '').trim();
  return text && text.length <= 500 ? text : undefined;
}
function safeError(value: unknown): string {
  return (value instanceof Error ? value.message : String(value ?? 'unknown error'))
    .replace(/[\r\n\t]+/g, ' ')
    .slice(0, 300);
}

function strictInteger(name: string, value: unknown, minimum: number, maximum: number): number {
  const text = typeof value === 'number' ? String(value) : String(value ?? '').trim();
  if (!/^-?\d+$/.test(text)) throw new BadRequestException(`${name} must be an integer`);
  const parsed = Number(text);
  if (!Number.isSafeInteger(parsed) || parsed < minimum || parsed > maximum) {
    throw new BadRequestException(`${name} must be between ${minimum} and ${maximum}`);
  }
  return parsed;
}

function strictEnvInteger(name: string, fallback: number, minimum: number, maximum: number): number {
  const raw = process.env[name]?.trim();
  if (raw === undefined || raw === '') return fallback;
  try {
    return strictInteger(name, raw, minimum, maximum);
  } catch (error) {
    throw new Error(error instanceof Error ? error.message : `${name} is invalid`);
  }
}

function boundedInteger(value: string | undefined, fallback: number, minimum: number, maximum: number): number {
  const parsed = Number.parseInt(value ?? '', 10);
  return Number.isInteger(parsed) && parsed >= minimum && parsed <= maximum ? parsed : fallback;
}
