import { Injectable, ServiceUnavailableException } from '@nestjs/common';

export type GatewayCapability = 'SEND_TEXT'|'SEND_IMAGE'|'SEND_VIDEO'|'SEND_DOCUMENT'|'DELIVERY_EVENTS'|'READ_EVENTS'|'INBOUND_MESSAGES';

@Injectable()
export class GatewayIdentityService {
  readonly provider = 'OPENWA' as const;
  readonly engine = requiredChoice('OPENWA_ENGINE', ['WHATSAPP_WEB_JS','BAILEYS'] as const);
  readonly gatewayPoolId = required('GATEWAY_POOL_ID');
  readonly gatewayPoolVersion = requiredPositiveInteger('GATEWAY_POOL_VERSION');
  readonly adapterVersion = required('GATEWAY_ADAPTER_VERSION');
  readonly nodeId = required('GATEWAY_NODE_ID');
  readonly nodeVersion = requiredPositiveInteger('GATEWAY_NODE_VERSION');
  readonly capabilities = parseCapabilities(process.env.GATEWAY_CAPABILITIES);

  assertReady() {
    if (process.env.NODE_ENV === 'production' && Buffer.byteLength(process.env.GATEWAY_COMMAND_SECRET ?? '') < 32) throw new ServiceUnavailableException('command signing secret is not configured');
    if (process.env.NODE_ENV === 'production' && Buffer.byteLength(process.env.GATEWAY_CALLBACK_SECRET ?? '') < 32) throw new ServiceUnavailableException('callback signing secret is not configured');
  }

  describe() { return { provider:this.provider, engine:this.engine, gatewayPoolId:this.gatewayPoolId, gatewayPoolVersion:this.gatewayPoolVersion, adapterVersion:this.adapterVersion, nodeId:this.nodeId, nodeVersion:this.nodeVersion, capabilities:this.capabilities }; }
}

function required(name:string): string { const value=process.env[name]?.trim(); if (!value && process.env.NODE_ENV==='production') throw new Error(`${name} is required`); return value || `development-${name.toLowerCase()}`; }
function requiredChoice<T extends readonly string[]>(name:string, allowed:T):T[number] { const value=(process.env[name]??allowed[0]).trim(); if (!allowed.includes(value as T[number])) throw new Error(`${name} is invalid`); return value as T[number]; }
function requiredPositiveInteger(name:string):number { const value=Number.parseInt(process.env[name] ?? (process.env.NODE_ENV==='production' ? '' : '1'),10); if(!Number.isInteger(value)||value<=0) throw new Error(`${name} must be a positive integer`); return value; }
function parseCapabilities(raw:string|undefined):GatewayCapability[] {
 const allowed = new Set<GatewayCapability>(['SEND_TEXT','SEND_IMAGE','SEND_VIDEO','SEND_DOCUMENT','DELIVERY_EVENTS','READ_EVENTS','INBOUND_MESSAGES']);
 const values=(raw ?? 'SEND_TEXT,SEND_IMAGE,SEND_VIDEO,SEND_DOCUMENT,DELIVERY_EVENTS,READ_EVENTS,INBOUND_MESSAGES').split(',').map(v=>v.trim().toUpperCase()).filter(Boolean) as GatewayCapability[];
 const unique=[...new Set(values)]; if (!unique.length || unique.some(v=>!allowed.has(v))) throw new Error('GATEWAY_CAPABILITIES contains an unsupported capability'); return unique.sort();
}
