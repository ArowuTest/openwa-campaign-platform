import { Controller, Get } from '@nestjs/common';
import { GatewayIdentityService } from './gateway-identity.service';
@Controller('/v1/gateway')
export class CapabilitiesController { constructor(private readonly identity:GatewayIdentityService){} @Get('/capabilities') capabilities(){ return this.identity.describe(); } }
