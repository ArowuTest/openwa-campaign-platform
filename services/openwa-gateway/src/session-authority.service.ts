import { ConflictException, Injectable, OnModuleInit, UnauthorizedException } from '@nestjs/common';
import { link, mkdir, open, readFile, rename, rm } from 'node:fs/promises';
import { join } from 'node:path';
import type { SendRequest } from './provider/messaging-provider';
import { GatewayIdentityService } from './gateway-identity.service';
import { RuntimeRegistrationService } from './runtime-registration.service';

type AuthorityRecord = {
  provider: string;
  engine: string;
  gatewayPoolId: string;
  gatewayPoolVersion: number;
  gatewayAdapterVersion: string;
  gatewayNodeId: string;
  gatewayNodeVersion: number;
  sessionId: string;
  sessionLeaseVersion: number;
  sessionConfigurationVersion: number;
  authorityExpiresAt: string;
  routeReference: string;
  updatedAt: string;
};

@Injectable()
export class SessionAuthorityService implements OnModuleInit {
  private readonly directory = String(process.env.GATEWAY_SESSION_AUTHORITY_DIR ?? '/data/gateway-session-authority');
  private readonly validationQueues = new Map<string, Promise<void>>();
  constructor(private readonly identity: GatewayIdentityService, private readonly runtime: RuntimeRegistrationService) {}
  assertOwned(sessionId: string, dispatchVersion?: number): void {
    this.runtime.assertSessionOwned(sessionId, dispatchVersion);
  }

  async onModuleInit() { await mkdir(this.directory, { recursive: true, mode: 0o700 }); }

  async assertNotTombstoned(sessionId: string): Promise<void> {
    const previous = this.validationQueues.get(sessionId) ?? Promise.resolve();
    const current = previous.catch(() => undefined).then(() => this.throwIfTombstoned(sessionId));
    this.validationQueues.set(sessionId, current);
    try { await current; }
    finally { if (this.validationQueues.get(sessionId) === current) this.validationQueues.delete(sessionId); }
  }

  async runIfNotTombstoned<T>(sessionId: string, operation: () => Promise<T>): Promise<T> {
    const previous = this.validationQueues.get(sessionId) ?? Promise.resolve();
    const current = previous.catch(() => undefined).then(async () => {
      await this.throwIfTombstoned(sessionId);
      return operation();
    });
    const gate = current.then(() => undefined, () => undefined);
    this.validationQueues.set(sessionId, gate);
    try { return await current; }
    finally { if (this.validationQueues.get(sessionId) === gate) this.validationQueues.delete(sessionId); }
  }

  async tombstone(sessionId: string): Promise<void> {
    const previous = this.validationQueues.get(sessionId) ?? Promise.resolve();
    const current = previous.catch(() => undefined).then(async () => {
      await this.writeTombstone(sessionId);
      await rm(this.path(sessionId), { force: true });
      await syncDirectory(this.directory);
    });
    this.validationQueues.set(sessionId, current);
    try { await current; }
    finally { if (this.validationQueues.get(sessionId) === current) this.validationQueues.delete(sessionId); }
  }
  async validate(request: SendRequest): Promise<void> {
    const previous = this.validationQueues.get(request.sessionId) ?? Promise.resolve();
    const current = previous.catch(() => undefined).then(() => this.validateSerial(request));
    this.validationQueues.set(request.sessionId, current);
    try {
      await current;
    } finally {
      if (this.validationQueues.get(request.sessionId) === current) this.validationQueues.delete(request.sessionId);
    }
  }

  async submitIfCurrent<T>(request: SendRequest, operation: () => Promise<T>): Promise<T> {
    const previous = this.validationQueues.get(request.sessionId) ?? Promise.resolve();
    const current = previous.catch(() => undefined).then(async () => {
      await this.validateSerial(request);
      return operation();
    });
    const gate = current.then(() => undefined, () => undefined);
    this.validationQueues.set(request.sessionId, gate);
    try {
      return await current;
    } finally {
      if (this.validationQueues.get(request.sessionId) === gate) this.validationQueues.delete(request.sessionId);
    }
  }
  private async validateSerial(request: SendRequest): Promise<void> {
    this.assertOwned(request.sessionId, request.sessionLeaseVersion);
    await this.throwIfTombstoned(request.sessionId);
    const now = Date.now();
    const expiry = Date.parse(request.authorityExpiresAt);
    const maximumHorizonMs = boundedInteger(process.env.GATEWAY_AUTHORITY_MAX_HORIZON_SECONDS, 900, 30, 3600) * 1000;
    if (!Number.isFinite(expiry) || expiry <= now || expiry - now > maximumHorizonMs) {
      throw new UnauthorizedException('session authority is expired or outside the permitted horizon');
    }
    if (request.provider !== this.identity.provider || request.engine !== this.identity.engine ||
        request.gatewayPoolId !== this.identity.gatewayPoolId || request.gatewayPoolVersion !== this.identity.gatewayPoolVersion ||
        request.gatewayAdapterVersion !== this.identity.adapterVersion || request.gatewayNodeId !== this.identity.nodeId ||
        request.gatewayNodeVersion !== this.identity.nodeVersion) {
      throw new UnauthorizedException('session authority does not match this gateway runtime');
    }
    const current: AuthorityRecord = {
      provider: request.provider,
      engine: request.engine,
      gatewayPoolId: request.gatewayPoolId,
      gatewayPoolVersion: request.gatewayPoolVersion,
      gatewayAdapterVersion: request.gatewayAdapterVersion,
      gatewayNodeId: request.gatewayNodeId,
      gatewayNodeVersion: request.gatewayNodeVersion,
      sessionId: request.sessionId,
      sessionLeaseVersion: request.sessionLeaseVersion,
      sessionConfigurationVersion: request.sessionConfigurationVersion,
      authorityExpiresAt: new Date(expiry).toISOString(),
      routeReference: request.routeReference,
      updatedAt: new Date(now).toISOString()
    };
    const existing = await this.read(request.sessionId);
    if (existing) {
      if (request.sessionLeaseVersion < existing.sessionLeaseVersion) throw new ConflictException('stale session lease fence');
      if (request.sessionLeaseVersion === existing.sessionLeaseVersion) {
        if (request.provider !== existing.provider || request.engine !== existing.engine ||
            request.sessionConfigurationVersion !== existing.sessionConfigurationVersion ||
            request.gatewayNodeVersion !== existing.gatewayNodeVersion || request.gatewayNodeId !== existing.gatewayNodeId ||
            request.gatewayPoolId !== existing.gatewayPoolId || request.gatewayPoolVersion !== existing.gatewayPoolVersion ||
            request.gatewayAdapterVersion !== existing.gatewayAdapterVersion) {
          throw new ConflictException('session authority evidence conflicts with the current fence');
        }
      }
      if (request.sessionLeaseVersion > existing.sessionLeaseVersion && request.sessionConfigurationVersion < existing.sessionConfigurationVersion) {
        throw new ConflictException('new lease cannot use an older session configuration');
      }
    }
    await this.replace(request.sessionId, current);
  }

  private async throwIfTombstoned(sessionId: string): Promise<void> {
    try {
      await readFile(this.tombstonePath(sessionId), 'utf8');
      throw new ConflictException('session authority is permanently tombstoned');
    } catch (error) {
      if ((error as NodeJS.ErrnoException)?.code === 'ENOENT') return;
      throw error;
    }
  }
  private async writeTombstone(sessionId: string): Promise<void> {
    await mkdir(this.directory, { recursive: true, mode: 0o700 });
    const path = this.tombstonePath(sessionId);
    const temporary = `${path}.${process.pid}.${Date.now()}.tmp`;
    let handle;
    try {
      handle = await open(temporary, 'wx', 0o600);
      await handle.writeFile(JSON.stringify({ sessionId, deletedAt: new Date().toISOString() }), { encoding: 'utf8' });
      await handle.sync();
      await handle.close();
      handle = undefined;
      try {
        await link(temporary, path);
      } catch (error) {
        if ((error as NodeJS.ErrnoException)?.code !== 'EEXIST') throw error;
      }
      await rm(temporary, { force: true });
      await syncDirectory(this.directory);
    } catch (error) {
      await handle?.close().catch(() => undefined);
      await rm(temporary, { force: true }).catch(() => undefined);
      throw error;
    }
  }  private async read(sessionId: string): Promise<AuthorityRecord | undefined> {
    try { return JSON.parse(await readFile(this.path(sessionId), 'utf8')) as AuthorityRecord; }
    catch (error) { if ((error as NodeJS.ErrnoException)?.code === 'ENOENT') return undefined; throw error; }
  }
  private async replace(sessionId: string, record: AuthorityRecord): Promise<void> {
    await mkdir(this.directory, { recursive: true, mode: 0o700 });
    const path = this.path(sessionId);
    const temporary = `${path}.${process.pid}.${Date.now()}.tmp`;
    let handle;
    try {
      handle = await open(temporary, 'wx', 0o600);
      await handle.writeFile(JSON.stringify(record), { encoding: 'utf8' });
      await handle.sync();
      await handle.close();
      handle = undefined;
      await rename(temporary, path);
      await syncDirectory(this.directory);
    } catch (error) {
      await handle?.close().catch(() => undefined);
      await rm(temporary, { force: true }).catch(() => undefined);
      throw error;
    }
  }
  private path(sessionId: string): string { return join(this.directory, `${safeName(sessionId)}.json`); }
  private tombstonePath(sessionId: string): string { return join(this.directory, `${safeName(sessionId)}.deleted.json`); }
}

function safeName(value: string): string {
  if (!/^[A-Za-z0-9:_-]{1,128}$/.test(value)) throw new TypeError('session identifier is unsafe');
  return Buffer.from(value).toString('base64url');
}
function boundedInteger(value: string | undefined, fallback: number, minimum: number, maximum: number): number {
  const parsed = Number.parseInt(value ?? '', 10);
  return Number.isInteger(parsed) && parsed >= minimum && parsed <= maximum ? parsed : fallback;
}
async function syncDirectory(directory: string): Promise<void> {
  const handle = await open(directory, 'r');
  try { await handle.sync(); } finally { await handle.close(); }
}
