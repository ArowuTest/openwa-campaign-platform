import { Injectable, Logger, OnModuleDestroy, OnModuleInit } from '@nestjs/common';
import { createHash, createHmac, randomBytes, randomUUID } from 'node:crypto';
import { isIP } from 'node:net';
import { readFile, readdir, statfs } from 'node:fs/promises';
import { GatewayIdentityService } from './gateway-identity.service';
import { GatewayObservabilityService } from './observability.service';

export interface ContainerResourceHealth {
  scope: 'CONTAINER';
  filesystemPath: string;
  diskTotalBytes: number;
  diskFreeBytes: number;
  diskAvailableBytes: number;
  inodesTotal: number;
  inodesFree: number;
  processId: number;
  processUptimeSeconds: number;
  openFileDescriptorCount: number;
  networkRxBytes: number;
  networkTxBytes: number;
  networkInterfaceCount: number;
}
@Injectable()
export class RuntimeRegistrationService implements OnModuleInit, OnModuleDestroy {
  private readonly logger = new Logger(RuntimeRegistrationService.name);
  private readonly configuredBootId = String(process.env.GATEWAY_BOOT_ID ?? '').trim();
  private readonly bootId = this.configuredBootId || randomUUID();
  private readonly bootStartedAt = new Date(Date.now() - process.uptime() * 1_000).toISOString();
  private readonly intervalMs = boundedInteger(process.env.GATEWAY_RUNTIME_HEARTBEAT_MS, 30_000, 5_000, 300_000);
  private timer?: NodeJS.Timeout;
  private inFlightPublish?: Promise<boolean>;
  private inFlightRuntimeState?: string;
  private inFlightAbort?: AbortController;
  private shuttingDown = false;
  private runtimeSequence = 0;
  private lastCPU = process.cpuUsage();
  private lastCPUAt = process.hrtime.bigint();

  constructor(private readonly identity: GatewayIdentityService, private readonly observability: GatewayObservabilityService) {}

  async onModuleInit(): Promise<void> {
    if (process.env.NODE_ENV === 'production') {
      if (this.configuredBootId) throw new Error('GATEWAY_BOOT_ID is not permitted in production; every process start requires a fresh boot ID');
      this.validateProductionBindAddress();
      const advertisedURL = this.advertisedInternalURL();
      this.validateProductionControlPlaneBoundary(advertisedURL);
      this.runtimeURL();
      const gatewayCapacity = capacitySetting(process.env.GATEWAY_CAPACITY, 'GATEWAY_CAPACITY', true);
      const engineSessionLimit = capacitySetting(process.env.OPENWA_MAX_CONCURRENT_SESSIONS, 'OPENWA_MAX_CONCURRENT_SESSIONS', true);
      if (gatewayCapacity !== engineSessionLimit) {
        throw new Error('GATEWAY_CAPACITY must match OPENWA_MAX_CONCURRENT_SESSIONS in production');
      }
    }
    if (!this.runtimeURL() || Buffer.byteLength(process.env.GATEWAY_RUNTIME_SECRET ?? '') < 32) {
      if (process.env.NODE_ENV === 'production') throw new Error('gateway runtime registration is not configured');
      return;
    }
    await this.publish();
    this.timer = setInterval(() => void this.publish(), this.intervalMs);
    this.timer.unref();
  }

  async onModuleDestroy(): Promise<void> {
    this.shuttingDown = true;
    if (this.timer) clearInterval(this.timer);
    for (let attempt = 0; attempt < 3; attempt += 1) {
      if (await this.publish('DRAINING')) return;
      if (attempt < 2) await new Promise(resolve => setTimeout(resolve, 100));
    }
  }

  private async publish(runtimeState?: string): Promise<boolean> {
    if (this.shuttingDown && String(runtimeState ?? '').toUpperCase() !== 'DRAINING') return false;
    if (this.inFlightPublish) {
      if (!runtimeState) return false;
      if (String(runtimeState).toUpperCase() === 'DRAINING' && this.inFlightRuntimeState !== 'DRAINING') this.inFlightAbort?.abort();
      await this.inFlightPublish;
      if (this.shuttingDown && String(runtimeState ?? '').toUpperCase() !== 'DRAINING') return false;
    }
    const controller = new AbortController();
    const operation = Promise.resolve().then(() => this.publishOnce(runtimeState, controller.signal));
    this.inFlightRuntimeState = String(runtimeState ?? 'READY').toUpperCase();
    this.inFlightAbort = controller;
    this.inFlightPublish = operation;
    try { return await operation; } finally {
      if (this.inFlightPublish === operation) { this.inFlightPublish = undefined; this.inFlightRuntimeState = undefined; this.inFlightAbort = undefined; }
    }
  }

  private async publishOnce(runtimeState?: string, cancellation?: AbortSignal): Promise<boolean> {
    const started = process.hrtime.bigint();
    try {
      const report = await this.report(runtimeState);
      if (cancellation?.aborted) throw new Error('runtime publication aborted');
      const body = Buffer.from(JSON.stringify(report));
      const timestamp = String(Math.floor(Date.now() / 1000));
      const nonce = randomBytes(24).toString('base64url');
      const digest = createHash('sha256').update(body).digest('hex');
      const signature = `sha256=${createHmac('sha256', process.env.GATEWAY_RUNTIME_SECRET ?? '').update(timestamp).update('\n').update(nonce).update('\n').update(digest).digest('hex')}`;
      const response = await fetch(this.runtimeURL(), {
        method: 'POST', body,
        headers: {
          'content-type': 'application/json',
          'x-gateway-runtime-timestamp': timestamp,
          'x-gateway-runtime-nonce': nonce,
          'x-gateway-runtime-signature': signature,
          ...(this.observability.traceparent() ? { traceparent: this.observability.traceparent()! } : {})
        },
        redirect: 'manual',
        signal: AbortSignal.any([AbortSignal.timeout(boundedInteger(process.env.GATEWAY_RUNTIME_TIMEOUT_MS, 5_000, 1_000, 30_000)), ...(cancellation ? [cancellation] : [])])
      });
      if (!response.ok) throw new Error(`control plane returned HTTP ${response.status}`);
      this.observability.increment('openwa_gateway_runtime_registration_total', { outcome: 'accepted' });
      this.observability.gauge('openwa_gateway_runtime_registration_last_success_seconds', Math.floor(Date.now() / 1000));
      return true;
    } catch (error) {
      this.observability.increment('openwa_gateway_runtime_registration_total', { outcome: 'failed' });
      this.logger.warn(`gateway runtime registration deferred: ${safeError(error)}`);
      return false;
    } finally {
      this.observability.observe('openwa_gateway_runtime_registration_duration_seconds', Number(process.hrtime.bigint() - started) / 1e9);
    }
  }

  private runtimeURL(): string {
    const explicit = String(process.env.GATEWAY_RUNTIME_REGISTRATION_URL ?? '').trim();
    if (explicit) {
      if (process.env.NODE_ENV === 'production') {
        throw new Error('GATEWAY_RUNTIME_REGISTRATION_URL is not permitted in production; use CONTROL_API_INTERNAL_URL');
      }
      return explicit;
    }
    const rawBase = String(process.env.CONTROL_API_INTERNAL_URL ?? '').trim();
    const base = process.env.NODE_ENV === 'production'
      ? this.productionCrossProviderURL('CONTROL_API_INTERNAL_URL', true).canonical
      : rawBase.replace(/\/+$/u, '');
    return base ? `${base}/api/v1/internal/gateway-nodes/${encodeURIComponent(this.identity.nodeId)}/runtime` : '';
  }

  private productionCrossProviderURL(key: 'CONTROL_API_INTERNAL_URL'|'CONTROL_API_CALLBACK_URL'|'CONTROL_API_INBOUND_URL'|'MEDIA_DOWNLOAD_BASE_URL', authorityOnly: boolean): { parsed: URL; hostname: string; canonical: string } {
    const raw = String(process.env[key] ?? '').trim();
    let parsed: URL;
    try { parsed = new URL(raw); }
    catch { throw new Error(`${key} must be a valid HTTPS cross-provider URL in production`); }
    const hostname = normalizeProductionHostname(parsed.hostname);
    const rawAuthority = /^https:\/\/([^/?#]*)/iu.exec(raw)?.[1] ?? '';
    const authorityHostPort = rawAuthority.slice(rawAuthority.lastIndexOf('@') + 1);
    const port = parsed.port ? Number.parseInt(parsed.port, 10) : undefined;
    if (
      parsed.protocol !== 'https:' || !hostname || parsed.username || parsed.password || parsed.hash ||
      authorityHostPort.endsWith(':') || (port !== undefined && (!Number.isInteger(port) || port < 1 || port > 65535)) ||
      (authorityOnly && (parsed.pathname !== '/' || parsed.search))
    ) {
      throw new Error(`${key} must be a canonical HTTPS ${authorityOnly ? 'authority-only control origin' : 'cross-provider endpoint'} in production`);
    }
    const authority = isIP(hostname) === 6 ? `[${hostname}]${parsed.port ? `:${parsed.port}` : ''}` : `${hostname}${parsed.port ? `:${parsed.port}` : ''}`;
    return { parsed, hostname, canonical: `https://${authority}${authorityOnly ? '' : `${parsed.pathname}${parsed.search}`}` };
  }

  private validateProductionControlPlaneBoundary(advertisedURL: string): void {
    const keys = ['CONTROL_API_INTERNAL_URL','CONTROL_API_CALLBACK_URL','CONTROL_API_INBOUND_URL','MEDIA_DOWNLOAD_BASE_URL'] as const;
    let controlHost = '';
    for (const key of keys) {
      const { hostname } = this.productionCrossProviderURL(key, key === 'CONTROL_API_INTERNAL_URL');
      if (isDisallowedProductionEndpoint(hostname)) throw new Error(`${key} must not use a Docker-local, loopback, or link-local cross-provider hostname`);
      if (!controlHost) controlHost = hostname;
      else if (hostname !== controlHost) throw new Error(`${key} must use the approved Railway control hostname`);
    }
    const allowed = String(process.env.SSRF_ALLOWED_HOSTS ?? '').split(',').map(normalizeProductionHostname).filter(Boolean);
    if (allowed.length !== 1 || allowed[0] !== controlHost) {
      throw new Error('SSRF_ALLOWED_HOSTS must contain exactly the approved Railway control hostname');
    }
    const gatewayHost = normalizeProductionHostname(new URL(advertisedURL).hostname);
    const gatewayAuthorities = new Set(String(process.env.GATEWAY_RUNTIME_ALLOWED_HOSTS ?? '').split(',').map(normalizeProductionHostname).filter(Boolean));
    if ([...gatewayAuthorities].some(isIPLikeProductionHostname)) {
      throw new Error('GATEWAY_RUNTIME_ALLOWED_HOSTS must contain DNS hostnames only in production');
    }
    if (gatewayHost === controlHost || gatewayAuthorities.has(controlHost)) {
      throw new Error('gateway authority must not alias the Railway control hostname');
    }
  }
  private validateProductionBindAddress(): void {
    const bind = normalizeProductionHostname(String(process.env.GATEWAY_BIND_ADDRESS ?? ''));
    if (!bind || !isIP(bind) || !isPrivateNetworkAddress(bind)) {
      throw new Error('production GATEWAY_BIND_ADDRESS must be an explicitly approved private IP address');
    }
  }

  private advertisedInternalURL(): string {
    const explicit = String(process.env.GATEWAY_INTERNAL_URL ?? '').trim().replace(/\/+$/u, '');
    if (!explicit) {
      if (process.env.NODE_ENV === 'production') throw new Error('GATEWAY_INTERNAL_URL is required in production');
      return `http://openwa-gateway:${process.env.PORT ?? '2785'}`;
    }
    let parsed: URL;
    try { parsed = new URL(explicit); }
    catch { throw new Error('GATEWAY_INTERNAL_URL must be a valid http or https URL'); }
    if ((parsed.protocol !== 'http:' && parsed.protocol !== 'https:') || !parsed.hostname) {
      throw new Error('GATEWAY_INTERNAL_URL must be a valid http or https URL');
    }
    if (parsed.username || parsed.password || parsed.pathname !== '/' || parsed.search || parsed.hash) {
      throw new Error('GATEWAY_INTERNAL_URL must contain only scheme and governed host authority');
    }
    const hostname = normalizeProductionHostname(parsed.hostname);
    const rawAuthority = /^[a-z][a-z0-9+.-]*:\/\/([^/?#]*)/iu.exec(explicit)?.[1] ?? '';
    const authorityHostPort = rawAuthority.slice(rawAuthority.lastIndexOf('@') + 1);
    const port = parsed.port ? Number.parseInt(parsed.port, 10) : undefined;
    if (authorityHostPort.endsWith(':') || (port !== undefined && (!Number.isInteger(port) || port < 1 || port > 65535))) {
      throw new Error('GATEWAY_INTERNAL_URL must use a valid port between 1 and 65535');
    }
    if (process.env.NODE_ENV === 'production') {
      if (parsed.protocol !== 'https:') throw new Error('production GATEWAY_INTERNAL_URL must use HTTPS');
      if (isDisallowedProductionEndpoint(hostname)) {
        throw new Error('GATEWAY_INTERNAL_URL must not advertise a Docker-local, loopback, or link-local production endpoint');
      }
      const allowed = new Set(String(process.env.GATEWAY_RUNTIME_ALLOWED_HOSTS ?? '').split(',').map(normalizeProductionHostname).filter(Boolean));
      if ([...allowed].some(isIPLikeProductionHostname)) {
        throw new Error('GATEWAY_RUNTIME_ALLOWED_HOSTS must contain DNS hostnames only in production');
      }
      if (allowed.size === 0 || !allowed.has(hostname)) {
        throw new Error('production GATEWAY_INTERNAL_URL hostname must be present in GATEWAY_RUNTIME_ALLOWED_HOSTS');
      }
    }
    const authority = isIP(hostname) === 6 ? `[${hostname}]${parsed.port ? `:${parsed.port}` : ''}` : `${hostname}${parsed.port ? `:${parsed.port}` : ''}`;
    return `${parsed.protocol.toLowerCase()}//${authority}`;
  }

  private async report(runtimeStateOverride?: string): Promise<Record<string, unknown>> {
    this.runtimeSequence += 1;
    if (!Number.isSafeInteger(this.runtimeSequence)) throw new Error('gateway runtime sequence exceeded the safe integer range');
    const capacity = runtimeCapacity(process.env.GATEWAY_CAPACITY);
    const directories = [
      String(process.env.GATEWAY_SESSION_AUTHORITY_DIR ?? '/data/gateway-session-authority'),
      String(process.env.GATEWAY_EVENT_OUTBOX_DIR ?? '/data/gateway-event-outbox'),
      String(process.env.GATEWAY_INBOUND_OUTBOX_DIR ?? '/data/gateway-inbound-outbox')
    ];
    const [sessionCount, eventDepth, inboundDepth] = await Promise.all(directories.map(countJSONFiles));
    const nowCPU = process.cpuUsage();
    const now = process.hrtime.bigint();
    const elapsedMicros = Number(now - this.lastCPUAt) / 1_000;
    const usedMicros = (nowCPU.user - this.lastCPU.user) + (nowCPU.system - this.lastCPU.system);
    const cpuPercent = elapsedMicros > 0 ? Math.min(100, Math.max(0, usedMicros / elapsedMicros * 100)) : 0;
    this.lastCPU = nowCPU; this.lastCPUAt = now;
    const identity = this.identity.describe();
    const resourceFilesystemPath = String(process.env.GATEWAY_RESOURCE_FILESYSTEM_PATH ?? process.env.GATEWAY_SESSION_DATA_DIR ?? '/data').trim() || '/data';
    const resourceHealth = await collectContainerResourceHealth(resourceFilesystemPath);
    return {
      nodeId: identity.nodeId,
      expectedNodeVersion: identity.nodeVersion,
      gatewayPoolId: identity.gatewayPoolId,
      provider: identity.provider,
      engine: identity.engine,
      adapterVersion: identity.adapterVersion,
      gatewayVersion: String(process.env.GATEWAY_VERSION ?? '0.8.28'),
      workerVersion: process.version,
      configurationVersion: String(process.env.GATEWAY_CONFIGURATION_VERSION ?? identity.gatewayPoolVersion),
      bootId: this.bootId,
      bootStartedAt: this.bootStartedAt,
      runtimeSequence: this.runtimeSequence,
      internalUrl: this.advertisedInternalURL(),
      capabilities: identity.capabilities,
      runtimeState: String(runtimeStateOverride ?? process.env.GATEWAY_RUNTIME_STATE ?? 'READY').toUpperCase(),
      capacity,
      sessionCount,
      queueDepth: eventDepth + inboundDepth,
      cpuPercent: Number(cpuPercent.toFixed(3)),
      memoryBytes: process.memoryUsage().rss,
      resourceHealth,
      observedAt: new Date().toISOString()
    };
  }
}

function normalizeProductionHostname(value: string): string { return value.trim().toLowerCase().replace(/^\[|\]$/gu, '').replace(/\.$/u, ''); }

function isDisallowedProductionEndpoint(hostname: string): boolean {
  const normalized = normalizeProductionHostname(hostname);
  if (
    normalized === 'control-api' || normalized === 'openwa-gateway' ||
    normalized === 'localhost' || normalized.endsWith('.localhost') ||
    normalized === 'host.docker.internal' || normalized.endsWith('.docker.internal')
  ) return true;
  // Production cross-provider and advertised gateway authority is DNS-only.
  // Private literals are reserved for GATEWAY_BIND_ADDRESS/local plumbing.
  return isIPLikeProductionHostname(normalized);
}

function isIPLikeProductionHostname(hostname: string): boolean {
  const normalized = normalizeProductionHostname(hostname);
  if (!normalized || normalized.includes('%') || mappedIPv4Octets(normalized) || isIP(normalized) !== 0) return Boolean(normalized);
  try {
    const canonical = normalizeProductionHostname(new URL(`http://${normalized}`).hostname);
    return canonical !== normalized && (mappedIPv4Octets(canonical) !== undefined || isIP(canonical) !== 0);
  } catch {
    return false;
  }
}

function isPrivateNetworkAddress(hostname: string): boolean {
  const normalized = hostname.toLowerCase().replace(/^\[|\]$/gu, '');
  const mapped = mappedIPv4Octets(normalized);
  if (mapped) return isPrivateIPv4Octets(mapped);
  const family = isIP(normalized);
  if (family === 4) {
    const octets = normalized.split('.').map(value => Number.parseInt(value, 10));
    return isPrivateIPv4Octets(octets);
  }
  return family === 6 && /^f[cd][0-9a-f]{2}:/iu.test(normalized);
}

function isPrivateIPv4Octets(octets: number[]): boolean {
  if (octets.length !== 4 || octets.some(value => !Number.isInteger(value) || value < 0 || value > 255)) return false;
  if (octets[0] === 10) return true;
  if (octets[0] === 192 && octets[1] === 168) return true;
  return octets[0] === 172 && octets[1] >= 16 && octets[1] <= 31;
}

function mappedIPv4Octets(hostname: string): number[] | undefined {
  const normalized = hostname.toLowerCase().replace(/^\[|\]$/gu, '');
  if (!normalized.startsWith('::ffff:')) return undefined;
  const tail = normalized.slice(7);
  if (tail.includes('.')) {
    const octets = tail.split('.').map(value => Number.parseInt(value, 10));
    return octets.length === 4 && octets.every(value => Number.isInteger(value) && value >= 0 && value <= 255) ? octets : undefined;
  }
  const parts = tail.split(':');
  if (parts.length !== 2 || parts.some(part => !/^[0-9a-f]{1,4}$/u.test(part))) return undefined;
  const high = Number.parseInt(parts[0], 16), low = Number.parseInt(parts[1], 16);
  return [high >>> 8, high & 0xff, low >>> 8, low & 0xff];
}

function measuredInteger(value: bigint, name: string): number {
  if (value < 0n || value > BigInt(Number.MAX_SAFE_INTEGER)) {
    throw new Error(`${name} is outside the safe integer range`);
  }
  return Number(value);
}

async function containerNetworkCounters(): Promise<{ rxBytes: number; txBytes: number; interfaceCount: number }> {
  const raw = await readFile('/proc/net/dev', 'utf8');
  let rxBytes = 0n;
  let txBytes = 0n;
  let interfaceCount = 0;
  for (const line of raw.split(/\r?\n/u)) {
    const separator = line.indexOf(':');
    if (separator < 0) continue;
    const name = line.slice(0, separator).trim();
    if (!name || name === 'lo') continue;
    const fields = line.slice(separator + 1).trim().split(/\s+/u);
    if (fields.length < 16) continue;
    try {
      rxBytes += BigInt(fields[0]);
      txBytes += BigInt(fields[8]);
      interfaceCount += 1;
    } catch { continue; }
  }
  if (interfaceCount < 1) throw new Error('no non-loopback container network interface is measurable');
  return { rxBytes: measuredInteger(rxBytes, 'networkRxBytes'), txBytes: measuredInteger(txBytes, 'networkTxBytes'), interfaceCount };
}
export async function collectContainerResourceHealth(filesystemPath: string): Promise<ContainerResourceHealth> {
  const measuredPath = filesystemPath.trim() || '/data';
  const [filesystem, descriptors, network] = await Promise.all([
    statfs(measuredPath, { bigint: true }),
    readdir('/proc/self/fd'),
    containerNetworkCounters()
  ]);
  const blockSize = filesystem.bsize;
  return {
    scope: 'CONTAINER',
    filesystemPath: measuredPath,
    diskTotalBytes: measuredInteger(blockSize * filesystem.blocks, 'diskTotalBytes'),
    diskFreeBytes: measuredInteger(blockSize * filesystem.bfree, 'diskFreeBytes'),
    diskAvailableBytes: measuredInteger(blockSize * filesystem.bavail, 'diskAvailableBytes'),
    inodesTotal: measuredInteger(filesystem.files, 'inodesTotal'),
    inodesFree: measuredInteger(filesystem.ffree, 'inodesFree'),
    processId: process.pid,
    processUptimeSeconds: Math.max(0, Math.floor(process.uptime())),
    openFileDescriptorCount: descriptors.length,
    networkRxBytes: network.rxBytes,
    networkTxBytes: network.txBytes,
    networkInterfaceCount: network.interfaceCount
  };
}
async function countJSONFiles(directory: string): Promise<number> {
  try { return (await readdir(directory)).filter(name => name.endsWith('.json')).length; }
  catch (error) { if ((error as NodeJS.ErrnoException)?.code === 'ENOENT') return 0; throw error; }
}
function safeError(value: unknown): string { return (value instanceof Error ? value.message : String(value ?? 'unknown')).replace(/[\r\n\t]+/gu, ' ').slice(0, 300); }
function runtimeCapacity(value: string | undefined): number {
  return capacitySetting(value, 'GATEWAY_CAPACITY', false);
}
function capacitySetting(value: string | undefined, name: string, required: boolean): number {
  const raw = String(value ?? '').trim();
  if (!raw) {
    if (required) throw new Error(`${name} is required in production`);
    return 1;
  }
  if (!/^\d+$/u.test(raw)) throw new Error(`${name} must be an integer between 1 and 1000`);
  const parsed = Number.parseInt(raw, 10);
  if (parsed < 1 || parsed > 1000) throw new Error(`${name} must be between 1 and 1000`);
  return parsed;
}
function boundedInteger(value: string | undefined, fallback: number, minimum: number, maximum: number): number {
  const parsed = Number.parseInt(value ?? '', 10);
  return Number.isInteger(parsed) && parsed >= minimum && parsed <= maximum ? parsed : fallback;
}
