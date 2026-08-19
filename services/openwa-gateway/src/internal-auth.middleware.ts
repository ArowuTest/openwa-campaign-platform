import { Injectable, NestMiddleware, UnauthorizedException } from '@nestjs/common';
import type { NextFunction, Request, Response } from 'express';
import { createHash, createHmac, timingSafeEqual } from 'node:crypto';
import { GatewayIdentityService } from './gateway-identity.service';
import { CommandReplayService } from './command-replay.service';

type RawRequest = Request & { rawBody?: Buffer };

@Injectable()
export class InternalAuthMiddleware implements NestMiddleware {
  constructor(private readonly identity: GatewayIdentityService, private readonly replay: CommandReplayService) {}

  async use(request: RawRequest, _response: Response, next: NextFunction): Promise<void> {
    const secrets = [process.env.GATEWAY_COMMAND_SECRET ?? '', process.env.GATEWAY_COMMAND_SECRET_PREVIOUS ?? '']
      .filter(secret => Buffer.byteLength(secret) >= 32);
    if (!secrets.length) {
      if (process.env.NODE_ENV === 'production') throw new UnauthorizedException('gateway command signing is not configured');
      next(); return;
    }

    const timestamp = request.header('x-gateway-timestamp') ?? '';
    const nonce = request.header('x-gateway-nonce') ?? '';
    const signature = request.header('x-gateway-signature') ?? '';
    const parsedTimestamp = Number.parseInt(timestamp, 10);
    const now = Math.floor(Date.now() / 1000);
    const windowSeconds = boundedInteger(process.env.GATEWAY_COMMAND_REPLAY_WINDOW_SECONDS, 300, 30, 900);
    if (!Number.isInteger(parsedTimestamp) || Math.abs(now - parsedTimestamp) > windowSeconds) throw new UnauthorizedException('gateway command timestamp is outside the replay window');
    if (!/^[A-Za-z0-9_-]{16,128}$/.test(nonce)) throw new UnauthorizedException('gateway command nonce is invalid');

    const contentLength = (request.header('content-length') ?? '').trim();
    const transferEncoding = (request.header('transfer-encoding') ?? '').trim();
    const parsedLength = contentLength === '' ? 0 : Number.parseInt(contentLength, 10);
    const declaredBody = transferEncoding !== '' || (Number.isInteger(parsedLength) && parsedLength > 0);
    const malformedLength = contentLength !== '' && !/^\d+$/.test(contentLength);
    if (!Buffer.isBuffer(request.rawBody) && (declaredBody || malformedLength)) {
      throw new UnauthorizedException('gateway command raw body evidence is unavailable');
    }
    const bodyHash = createHash('sha256').update(request.rawBody ?? Buffer.alloc(0)).digest('hex');
    const canonicalFields = [request.method.toUpperCase(), request.originalUrl, timestamp, nonce, bodyHash];
    if (request.path.startsWith('/v1/sessions')) {
      canonicalFields.push(
        (request.header('x-gateway-target-node-id') ?? '').trim(),
        (request.header('x-gateway-target-node-version') ?? '').trim(),
        (request.header('x-gateway-target-pool-id') ?? '').trim(),
        (request.header('x-gateway-target-provider') ?? '').trim().toUpperCase(),
        (request.header('x-gateway-target-engine') ?? '').trim().toUpperCase(),
        (request.header('x-gateway-target-adapter-version') ?? '').trim(),
      );
    }
    const canonical = canonicalFields.join('\n');
    const valid = secrets.some(secret => safeEqual(`sha256=${createHmac('sha256', secret).update(canonical).digest('hex')}`, signature));
    if (!valid) throw new UnauthorizedException('invalid gateway command signature');
    if (request.path.startsWith('/v1/sessions')) this.assertTargetIdentity(request);

    // Persist only after authentication succeeds. Atomic exclusive creation on a
    // shared durable volume makes replay rejection restart-safe and multi-process safe.
    await this.replay.claim(nonce, createHash('sha256').update(canonical).digest('hex'), now, parsedTimestamp + windowSeconds);
    next();
  }

  private assertTargetIdentity(request: Request) {
    const expected = this.identity.describe();
    const nodeVersion = Number.parseInt(request.header('x-gateway-target-node-version') ?? '', 10);
    if (request.header('x-gateway-target-node-id') !== expected.nodeId || nodeVersion !== expected.nodeVersion) throw new UnauthorizedException('gateway node target does not match runtime identity');
    if (request.header('x-gateway-target-pool-id') !== expected.gatewayPoolId) throw new UnauthorizedException('gateway pool target does not match runtime identity');
    if ((request.header('x-gateway-target-provider') ?? '').toUpperCase() !== expected.provider) throw new UnauthorizedException('gateway provider target does not match runtime identity');
    if ((request.header('x-gateway-target-engine') ?? '').toUpperCase() !== expected.engine) throw new UnauthorizedException('gateway engine target does not match runtime identity');
    if (request.header('x-gateway-target-adapter-version') !== expected.adapterVersion) throw new UnauthorizedException('gateway adapter target does not match runtime identity');
  }
}

function safeEqual(left: string, right: string): boolean {
  const a = Buffer.from(left); const b = Buffer.from(right);
  return a.length === b.length && timingSafeEqual(a, b);
}
function boundedInteger(value: string | undefined, fallback: number, minimum: number, maximum: number): number {
  const parsed = Number.parseInt(value ?? '', 10);
  return Number.isInteger(parsed) && parsed >= minimum && parsed <= maximum ? parsed : fallback;
}
