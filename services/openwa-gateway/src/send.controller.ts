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
    const capability = ({text:'SEND_TEXT',image:'SEND_IMAGE',video:'SEND_VIDEO',document:'SEND_DOCUMENT'} as const)[request.messageType];
    if(!this.identity.capabilities.includes(capability)) throw new BadRequestException(`gateway does not support ${capability}`);
    return this.gateway.send(request);
  }
}
function validate(body: SendBody): SendRequest {
  const required: Array<keyof SendRequest> = ['idempotencyKey', 'gatewayPoolId', 'sessionId', 'recipientMsisdn', 'messageType'];
  for (const key of required) if (typeof body[key] !== 'string' || String(body[key]).trim() === '') throw new BadRequestException(`${key} is required`);
  if (!/^[A-Za-z0-9:_-]{16,200}$/.test(String(body.idempotencyKey))) throw new BadRequestException('idempotencyKey format is invalid');
  if (!/^[A-Za-z0-9:_-]{1,128}$/.test(String(body.gatewayPoolId))) throw new BadRequestException('gatewayPoolId format is invalid');
  if (!/^[A-Za-z0-9:_-]{1,128}$/.test(String(body.sessionId))) throw new BadRequestException('sessionId format is invalid');
  if (!/^\+[1-9]\d{7,14}$/.test(String(body.recipientMsisdn))) throw new BadRequestException('recipientMsisdn must be in E.164 format');
  const supported: MessageType[] = ['text', 'image', 'video', 'document'];
  if (!supported.includes(body.messageType as MessageType)) throw new BadRequestException('unsupported messageType');
  if (body.messageType === 'text' && (!body.body || body.body.trim() === '')) throw new BadRequestException('text messages require body');
  if (body.messageType !== 'text' && (!body.mediaUrl || body.mediaUrl.trim() === '')) throw new BadRequestException('media messages require mediaUrl');
  if (body.body && body.body.length > 16_384) throw new BadRequestException('message body exceeds limit');
  return { idempotencyKey: String(body.idempotencyKey), gatewayPoolId: String(body.gatewayPoolId), sessionId: String(body.sessionId), recipientMsisdn: String(body.recipientMsisdn), messageType: body.messageType as MessageType, body: body.body?.trim(), mediaUrl: body.mediaUrl?.trim(), clientReference: body.clientReference?.trim() };
}
