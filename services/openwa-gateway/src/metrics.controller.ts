import { Controller, Get, Res } from '@nestjs/common';
import type { Response } from 'express';
import { GatewayObservabilityService } from './observability.service';

@Controller()
export class MetricsController {
  constructor(private readonly observability: GatewayObservabilityService) {}
  @Get('/metrics')
  metrics(@Res() response: Response): void {
    response.setHeader('content-type', 'text/plain; version=0.0.4; charset=utf-8');
    response.setHeader('cache-control', 'no-store');
    response.send(this.observability.prometheus());
  }
}
