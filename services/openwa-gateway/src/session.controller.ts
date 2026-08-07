import { BadRequestException, Body, Controller, Delete, Get, Param, Post } from '@nestjs/common';
import { GatewayMessagingService } from './gateway-messaging.service';
import type { SessionStartOptions } from './provider/messaging-provider';

@Controller('/v1/sessions')
export class SessionController {
  constructor(private readonly gateway: GatewayMessagingService) {}
  @Get('/:id') get(@Param('id') id: string) { return this.gateway.getSession(validateSessionId(id)); }
  @Get('/:id/health') health(@Param('id') id: string) { return this.gateway.health(validateSessionId(id)); }
  @Post() create(@Body() body: { name?: string }) { return this.gateway.createSession(validateSessionId(body?.name ?? '')); }
  @Post('/:id/start') start(@Param('id') id: string, @Body() body?: SessionStartOptions) { return this.gateway.startSession(validateSessionId(id), body); }
  @Post('/:id/stop') stop(@Param('id') id: string) { return this.gateway.stopSession(validateSessionId(id)); }
  @Post('/:id/logout') logout(@Param('id') id: string) { return this.gateway.logoutSession(validateSessionId(id)); }
  @Delete('/:id') remove(@Param('id') id: string) { return this.gateway.deleteSession(validateSessionId(id)); }
  @Get('/:id/qr') qr(@Param('id') id: string) { return this.gateway.qr(validateSessionId(id)); }
  @Post('/:id/pairing-code') pairingCode(@Param('id') id: string, @Body() body: { phoneNumber?: string }) {
    const phone = String(body?.phoneNumber ?? '').replace(/\D/g, '');
    if (!/^\d{8,15}$/.test(phone)) throw new BadRequestException('phoneNumber must contain 8 to 15 digits');
    return this.gateway.pairingCode(validateSessionId(id), phone);
  }
  @Post('/:id/drain') drain(@Param('id') id: string) { id = validateSessionId(id); this.gateway.drain(id); return { sessionId: id, draining: true }; }
  @Post('/:id/resume') resume(@Param('id') id: string) { id = validateSessionId(id); this.gateway.resume(id); return { sessionId: id, draining: false }; }
}

function validateSessionId(value: string): string {
  value = String(value ?? '').trim();
  if (!/^[A-Za-z0-9_-]{3,50}$/.test(value)) throw new BadRequestException('session identifier format is invalid');
  return value;
}
