import { BadRequestException, Body, Controller, Post } from '@nestjs/common';
import { GatewayMessagingService } from './gateway-messaging.service';
import { GatewayIdentityService } from './gateway-identity.service';
import type { MessageType, SendRequest } from './provider/messaging-provider';

type SendBody = Partial<SendRequest>;
@Controller('/v1/messages')
export class SendController {
  constructor(private readonly gateway: GatewayMessagingService, private readonly identity: GatewayIdentityService) {}
  @Post()
  send(@Body() body: SendBody) {
    const request=validate(body);
    if(request.gatewayPoolId!==this.identity.gatewayPoolId) throw new BadRequestException('gatewayPoolId does not match this gateway instance');
    if(request.gatewayPoolVersion!==this.identity.gatewayPoolVersion) throw new BadRequestException('gatewayPoolVersion does not match this gateway instance');
    if(request.gatewayAdapterVersion!==this.identity.adapterVersion) throw new BadRequestException('gatewayAdapterVersion does not match this gateway instance');
    if(request.gatewayNodeId!==this.identity.nodeId || request.gatewayNodeVersion!==this.identity.nodeVersion) throw new BadRequestException('gateway node authority does not match this gateway instance');
    if(request.provider!==this.identity.provider || request.engine!==this.identity.engine) throw new BadRequestException('provider or engine does not match this gateway instance');
    const capability = ({text:'SEND_TEXT',image:'SEND_IMAGE',video:'SEND_VIDEO',document:'SEND_DOCUMENT'} as const)[request.messageType];
    if(!this.identity.capabilities.includes(capability)) throw new BadRequestException(`gateway does not support ${capability}`);
    return this.gateway.send(request);
  }
}
function validate(body: SendBody): SendRequest {
  const required: Array<keyof SendRequest> = ['idempotencyKey', 'provider', 'engine', 'gatewayPoolId', 'gatewayAdapterVersion', 'gatewayNodeId', 'sessionId', 'authorityExpiresAt', 'routeReference', 'recipientMsisdn', 'messageType'];
  for (const key of required) if (typeof body[key] !== 'string' || String(body[key]).trim() === '') throw new BadRequestException(`${key} is required`);
  if (!/^[A-Za-z0-9:_-]{16,200}$/.test(String(body.idempotencyKey))) throw new BadRequestException('idempotencyKey format is invalid');
  if (!/^[A-Za-z0-9:_-]{1,128}$/.test(String(body.gatewayPoolId))) throw new BadRequestException('gatewayPoolId format is invalid');
  if (body.provider !== 'OPENWA') throw new BadRequestException('provider must be OPENWA');
  if (body.engine !== 'WHATSAPP_WEB_JS' && body.engine !== 'BAILEYS') throw new BadRequestException('engine is invalid');
  for (const field of ['gatewayPoolVersion','gatewayNodeVersion','sessionLeaseVersion','sessionConfigurationVersion'] as const) {
    if (!Number.isInteger(body[field]) || Number(body[field]) <= 0) throw new BadRequestException(`${field} must be a positive integer`);
  }
  if (!/^[A-Za-z0-9._:+-]{1,128}$/.test(String(body.gatewayAdapterVersion))) throw new BadRequestException('gatewayAdapterVersion format is invalid');
  if (!/^[A-Za-z0-9:_-]{1,128}$/.test(String(body.gatewayNodeId))) throw new BadRequestException('gatewayNodeId format is invalid');
  if (!/^[A-Za-z0-9:_-]{1,128}$/.test(String(body.sessionId))) throw new BadRequestException('sessionId format is invalid');
  if (Number.isNaN(Date.parse(String(body.authorityExpiresAt)))) throw new BadRequestException('authorityExpiresAt is invalid');
  if (!/^[A-Za-z0-9._:-]{1,200}$/.test(String(body.routeReference))) throw new BadRequestException('routeReference format is invalid');
  if (!/^\+[1-9]\d{7,14}$/.test(String(body.recipientMsisdn))) throw new BadRequestException('recipientMsisdn must be in E.164 format');
  const supported: MessageType[] = ['text', 'image', 'video', 'document'];
  if (!supported.includes(body.messageType as MessageType)) throw new BadRequestException('unsupported messageType');
  if (body.messageType === 'text' && (!body.body || body.body.trim() === '')) throw new BadRequestException('text messages require body');
  if (body.messageType !== 'text' && (!body.mediaUrl || body.mediaUrl.trim() === '')) throw new BadRequestException('media messages require mediaUrl');
  if (body.body && body.body.length > 16_384) throw new BadRequestException('message body exceeds limit');
  return {
    idempotencyKey: String(body.idempotencyKey), provider: 'OPENWA', engine: body.engine as SendRequest['engine'],
    gatewayPoolId: String(body.gatewayPoolId), gatewayPoolVersion: Number(body.gatewayPoolVersion),
    gatewayAdapterVersion: String(body.gatewayAdapterVersion), gatewayNodeId: String(body.gatewayNodeId), gatewayNodeVersion: Number(body.gatewayNodeVersion),
    sessionId: String(body.sessionId), sessionLeaseVersion: Number(body.sessionLeaseVersion), sessionConfigurationVersion: Number(body.sessionConfigurationVersion),
    authorityExpiresAt: new Date(String(body.authorityExpiresAt)).toISOString(), routeReference: String(body.routeReference),
    recipientMsisdn: String(body.recipientMsisdn), messageType: body.messageType as MessageType,
    body: body.body?.trim(), mediaUrl: body.mediaUrl?.trim(), clientReference: body.clientReference?.trim()
  };
}
