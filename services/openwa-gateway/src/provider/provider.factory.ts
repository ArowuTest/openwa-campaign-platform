import { Provider } from '@nestjs/common';
import { MESSAGING_PROVIDER } from './provider.token';
import { MockMessagingProvider } from './mock.provider';
import { OpenWAProvider } from './openwa.provider';

export function messagingProviderRegistration(): Provider {
  const mode = String(process.env.GATEWAY_PROVIDER_MODE ?? (process.env.NODE_ENV === 'production' ? 'openwa' : 'mock')).trim().toLowerCase();
  if (mode === 'openwa') return { provide: MESSAGING_PROVIDER, useExisting: OpenWAProvider };
  if (mode === 'mock' && process.env.NODE_ENV !== 'production') return { provide: MESSAGING_PROVIDER, useExisting: MockMessagingProvider };
  throw new Error(`unsupported or unsafe GATEWAY_PROVIDER_MODE ${mode}`);
}
