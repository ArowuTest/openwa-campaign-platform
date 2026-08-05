import { Injectable, NestMiddleware, UnauthorizedException } from '@nestjs/common';
import type { NextFunction, Request, Response } from 'express';
import { createHash, createHmac, timingSafeEqual } from 'node:crypto';

type RawRequest = Request & { rawBody?: Buffer };

@Injectable()
export class InternalAuthMiddleware implements NestMiddleware {
  private readonly seenNonces = new Map<string, number>();

  use(request: RawRequest, _response: Response, next: NextFunction) {
    const secret = process.env.GATEWAY_COMMAND_SECRET ?? '';
    if (Buffer.byteLength(secret) < 32) {
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
    this.expireNonces(now - windowSeconds);
    if (this.seenNonces.has(nonce)) throw new UnauthorizedException('gateway command nonce was already used');

    const bodyHash = createHash('sha256').update(request.rawBody ?? Buffer.alloc(0)).digest('hex');
    const canonical = [request.method.toUpperCase(), request.originalUrl, timestamp, nonce, bodyHash].join('\n');
    const expected = `sha256=${createHmac('sha256', secret).update(canonical).digest('hex')}`;
    if (!safeEqual(expected, signature)) throw new UnauthorizedException('invalid gateway command signature');
    this.seenNonces.set(nonce, now);
    next();
  }

  private expireNonces(threshold: number) { for (const [nonce, seenAt] of this.seenNonces) if (seenAt < threshold) this.seenNonces.delete(nonce); }
}

function safeEqual(left: string, right: string): boolean {
  const a = Buffer.from(left); const b = Buffer.from(right);
  return a.length === b.length && timingSafeEqual(a, b);
}
function boundedInteger(value: string | undefined, fallback: number, minimum: number, maximum: number): number {
  const parsed = Number.parseInt(value ?? '', 10);
  return Number.isInteger(parsed) && parsed >= minimum && parsed <= maximum ? parsed : fallback;
}
