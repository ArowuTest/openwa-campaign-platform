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
  private readonly bootId = String(process.env.GATEWAY_BOOT_ID ?? randomUUID());
  private readonly intervalMs = boundedInteger(process.env.GATEWAY_RUNTIME_HEARTBEAT_MS, 30_000, 5_000, 300_000);
  private timer?: NodeJS.Timeout;
  private running = false;
  private lastCPU = process.cpuUsage();
  private lastCPUAt = process.hrtime.bigint();

  constructor(private readonly identity: GatewayIdentityService, private readonly observability: GatewayObservabilityService) {}

  async onModuleInit(): Promise<void> {
    if (process.env.NODE_ENV === 'production') {
      this.advertisedInternalURL();
      this.validateProductionControlPlaneBoundary();
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
    if (this.timer) clearInterval(this.timer);
    await this.publish().catch(() => undefined);
  }

  private async publish(): Promise<void> {
    if (this.running) return;
    this.running = true;
    const started = process.hrtime.bigint();
    try {
      const body = Buffer.from(JSON.stringify(await this.report()));
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
        signal: AbortSignal.timeout(boundedInteger(process.env.GATEWAY_RUNTIME_TIMEOUT_MS, 5_000, 1_000, 30_000))
      });
      if (!response.ok) throw new Error(`control plane returned HTTP ${response.status}`);
      this.observability.increment('openwa_gateway_runtime_registration_total', { outcome: 'accepted' });
      this.observability.gauge('openwa_gateway_runtime_registration_last_success_seconds', Math.floor(Date.now() / 1000));
    } catch (error) {
      this.observability.increment('openwa_gateway_runtime_registration_total', { outcome: 'failed' });
      this.logger.warn(`gateway runtime registration deferred: ${safeError(error)}`);
    } finally {
      this.observability.observe('openwa_gateway_runtime_registration_duration_seconds', Number(process.hrtime.bigint() - started) / 1e9);
      this.running = false;
    }
  }

  private runtimeURL(): string {
    const explicit = String(process.env.GATEWAY_RUNTIME_REGISTRATION_URL ?? '').trim();
    if (explicit) return explicit;
    const base = String(process.env.CONTROL_API_INTERNAL_URL ?? '').trim().replace(/\/+$/u, '');
    return base ? `${base}/api/v1/internal/gateway-nodes/${encodeURIComponent(this.identity.nodeId)}/runtime` : '';
  }

  private validateProductionControlPlaneBoundary(): void {
    const keys = ['CONTROL_API_INTERNAL_URL','CONTROL_API_CALLBACK_URL','CONTROL_API_INBOUND_URL','MEDIA_DOWNLOAD_BASE_URL'] as const;
    let controlHost = '';
    for (const key of keys) {
      const raw = String(process.env[key] ?? '').trim();
      let parsed: URL;
      try { parsed = new URL(raw); }
      catch { throw new Error(`${key} must be a valid HTTPS cross-provider URL in production`); }
      if (parsed.protocol !== 'https:' || !parsed.hostname) {
        throw new Error(`${key} must be a valid HTTPS cross-provider URL in production`);
      }
      const hostname = parsed.hostname.toLowerCase().replace(/^\[|\]$/gu, '');
      if (isDisallowedProductionEndpoint(hostname)) throw new Error(`${key} must not use a Docker-local, loopback, or link-local cross-provider hostname`);
      if (!controlHost) controlHost = hostname;
      else if (hostname !== controlHost) throw new Error(`${key} must use the approved Railway control hostname`);
    }
    const allowed = String(process.env.SSRF_ALLOWED_HOSTS ?? '').split(',').map(value => value.trim().toLowerCase()).filter(Boolean);
    if (allowed.length !== 1 || allowed[0] !== controlHost) {
      throw new Error('SSRF_ALLOWED_HOSTS must contain exactly the approved Railway control hostname');
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
    const hostname = parsed.hostname.toLowerCase().replace(/^\[|\]$/gu, '');
    if (process.env.NODE_ENV === 'production') {
      if (isDisallowedProductionEndpoint(hostname)) {
        throw new Error('GATEWAY_INTERNAL_URL must not advertise a Docker-local, loopback, or link-local production endpoint');
      }
      if (parsed.protocol === 'http:') {
        if (!isPrivateNetworkAddress(hostname)) throw new Error('production HTTP GATEWAY_INTERNAL_URL must use a private network address');
        const bind = String(process.env.GATEWAY_BIND_ADDRESS ?? '').trim().toLowerCase().replace(/^\[|\]$/gu, '');
        if (!bind || !isIP(bind) || bind !== hostname) {
          throw new Error('production HTTP GATEWAY_INTERNAL_URL must match GATEWAY_BIND_ADDRESS');
        }
      }
    }
    return explicit;
  }

  private async report(): Promise<Record<string, unknown>> {
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
      internalUrl: this.advertisedInternalURL(),
      capabilities: identity.capabilities,
      runtimeState: String(process.env.GATEWAY_RUNTIME_STATE ?? 'READY').toUpperCase(),
      capacity: boundedInteger(process.env.GATEWAY_CAPACITY, 1, 1, 100_000),
      sessionCount,
      queueDepth: eventDepth + inboundDepth,
      cpuPercent: Number(cpuPercent.toFixed(3)),
      memoryBytes: process.memoryUsage().rss,
      resourceHealth,
      observedAt: new Date().toISOString()
    };
  }
}

function isDisallowedProductionEndpoint(hostname: string): boolean {
  const normalized = hostname.toLowerCase().replace(/^\[|\]$/gu, '');
  if (
    normalized === 'control-api' || normalized === 'openwa-gateway' ||
    normalized === 'localhost' || normalized.endsWith('.localhost') ||
    normalized === 'host.docker.internal' || normalized.endsWith('.docker.internal')
  ) return true;
  const family = isIP(normalized);
  if (family === 4) {
    const octets = normalized.split('.').map(value => Number.parseInt(value, 10));
    return octets[0] === 0 || octets[0] === 127 || (octets[0] === 169 && octets[1] === 254);
  }
  return family === 6 && (normalized === '::' || normalized === '::1' || /^fe[89ab][0-9a-f]:/iu.test(normalized));
}

function isPrivateNetworkAddress(hostname: string): boolean {
  const normalized = hostname.toLowerCase().replace(/^\[|\]$/gu, '');
  const family = isIP(normalized);
  if (family === 4) {
    const octets = normalized.split('.').map(value => Number.parseInt(value, 10));
    if (octets[0] === 10) return true;
    if (octets[0] === 192 && octets[1] === 168) return true;
    return octets[0] === 172 && octets[1] >= 16 && octets[1] <= 31;
  }
  return family === 6 && /^f[cd][0-9a-f]{2}:/iu.test(normalized);
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
function boundedInteger(value: string | undefined, fallback: number, minimum: number, maximum: number): number {
  const parsed = Number.parseInt(value ?? '', 10);
  return Number.isInteger(parsed) && parsed >= minimum && parsed <= maximum ? parsed : fallback;
}
