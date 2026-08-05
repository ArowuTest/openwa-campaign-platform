import { Controller, Get } from '@nestjs/common';
import { GatewayIdentityService } from './gateway-identity.service';
@Controller()
export class HealthController { constructor(private readonly identity:GatewayIdentityService){} @Get('/healthz') health(){ this.identity.assertReady(); return { status:'ok', ...this.identity.describe() }; } }
