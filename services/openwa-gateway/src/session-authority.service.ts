import { ConflictException, Injectable, OnModuleInit, UnauthorizedException } from '@nestjs/common';
import { mkdir, readFile, rename, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import type { SendRequest } from './provider/messaging-provider';
import { GatewayIdentityService } from './gateway-identity.service';

type AuthorityRecord = {
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
  constructor(private readonly identity: GatewayIdentityService) {}

  async onModuleInit() { await mkdir(this.directory, { recursive: true, mode: 0o700 }); }

  async validate(request: SendRequest): Promise<void> {
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
        if (request.sessionConfigurationVersion !== existing.sessionConfigurationVersion ||
            request.gatewayNodeVersion !== existing.gatewayNodeVersion || request.gatewayNodeId !== existing.gatewayNodeId ||
            request.gatewayPoolVersion !== existing.gatewayPoolVersion) {
          throw new ConflictException('session authority evidence conflicts with the current fence');
        }
      }
      if (request.sessionLeaseVersion > existing.sessionLeaseVersion && request.sessionConfigurationVersion < existing.sessionConfigurationVersion) {
        throw new ConflictException('new lease cannot use an older session configuration');
      }
    }
    await this.replace(request.sessionId, current);
  }

  private async read(sessionId: string): Promise<AuthorityRecord | undefined> {
    try { return JSON.parse(await readFile(this.path(sessionId), 'utf8')) as AuthorityRecord; }
    catch (error) { if ((error as NodeJS.ErrnoException)?.code === 'ENOENT') return undefined; throw error; }
  }
  private async replace(sessionId: string, record: AuthorityRecord): Promise<void> {
    await mkdir(this.directory, { recursive: true, mode: 0o700 });
    const path = this.path(sessionId); const temporary = `${path}.${process.pid}.${Date.now()}.tmp`;
    await writeFile(temporary, JSON.stringify(record), { encoding: 'utf8', mode: 0o600, flag: 'wx' });
    await rename(temporary, path);
  }
  private path(sessionId: string): string { return join(this.directory, `${safeName(sessionId)}.json`); }
}

function safeName(value: string): string {
  if (!/^[A-Za-z0-9:_-]{1,128}$/.test(value)) throw new TypeError('session identifier is unsafe');
  return Buffer.from(value).toString('base64url');
}
function boundedInteger(value: string | undefined, fallback: number, minimum: number, maximum: number): number {
  const parsed = Number.parseInt(value ?? '', 10);
  return Number.isInteger(parsed) && parsed >= minimum && parsed <= maximum ? parsed : fallback;
}
