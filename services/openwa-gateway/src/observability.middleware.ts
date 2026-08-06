import { Injectable, NestMiddleware } from '@nestjs/common';
import type { NextFunction, Request, Response } from 'express';
import { GatewayObservabilityService } from './observability.service';

@Injectable()
export class ObservabilityMiddleware implements NestMiddleware {
  constructor(private readonly observability: GatewayObservabilityService) {}
  use(request: Request, response: Response, next: NextFunction): void {
    const started = process.hrtime.bigint();
    this.observability.run(request.header('traceparent'), () => {
      const traceparent = this.observability.traceparent();
      if (traceparent) response.setHeader('traceparent', traceparent);
      response.once('finish', () => {
        const duration = Number(process.hrtime.bigint() - started) / 1_000_000_000;
        this.observability.observeHttp(request.method, request.route?.path ? `${request.method} ${request.route.path}` : request.path, response.statusCode, duration);
      });
      next();
    });
  }
}
