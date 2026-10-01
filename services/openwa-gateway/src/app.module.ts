import { MiddlewareConsumer, Module, NestModule, RequestMethod } from '@nestjs/common';
import { HealthController } from './health.controller';
import { GatewayMessagingService } from './gateway-messaging.service';
import { IdempotencyService } from './idempotency.service';
import { SessionPipelineService } from './session-pipeline.service';
import { InternalAuthMiddleware } from './internal-auth.middleware';
import { MockMessagingProvider } from './provider/mock.provider';
import { OpenWAProvider } from './provider/openwa.provider';
import { messagingProviderRegistration } from './provider/provider.factory';
import { SendController } from './send.controller';
import { ProviderEventPublisherService } from './provider-event-publisher.service';
import { SessionController } from './session.controller';
import { GatewayIdentityService } from './gateway-identity.service';
import { CapabilitiesController } from './capabilities.controller';
import { ProviderEventOutboxService } from './provider-event-outbox.service';
import { SessionAuthorityService } from './session-authority.service';
import { InboundMessageOutboxService } from './inbound-message-outbox.service';
import { InboundMessagePublisherService } from './inbound-message-publisher.service';
import { CommandReplayService } from './command-replay.service';
import { IdempotencyController } from './idempotency.controller';
import { GatewayObservabilityService } from './observability.service';
import { ObservabilityMiddleware } from './observability.middleware';
import { MetricsController } from './metrics.controller';
import { RuntimeRegistrationService } from './runtime-registration.service';
import { SessionHeartbeatPublisherService } from './session-heartbeat-publisher.service';
import { EmbeddedOpenWAEngineService } from './provider/embedded-openwa-engine.service';

@Module({
  controllers: [HealthController, SendController, SessionController, CapabilitiesController, IdempotencyController, MetricsController],
  providers: [
    GatewayIdentityService,
    GatewayObservabilityService,
    RuntimeRegistrationService,
    SessionHeartbeatPublisherService,
    CommandReplayService,
    MockMessagingProvider,
    OpenWAProvider,
    EmbeddedOpenWAEngineService,
    IdempotencyService,
    SessionPipelineService,
    GatewayMessagingService,
    ProviderEventOutboxService,
    ProviderEventPublisherService,
    SessionAuthorityService,
    InboundMessageOutboxService,
    InboundMessagePublisherService,
    messagingProviderRegistration()
  ]
})
export class AppModule implements NestModule {
  configure(consumer: MiddlewareConsumer) {
    consumer.apply(ObservabilityMiddleware).forRoutes("*");
    consumer.apply(InternalAuthMiddleware).exclude(
      { path: 'healthz', method: RequestMethod.GET },
      { path: 'metrics', method: RequestMethod.GET }
    ).forRoutes('*');
  }
}
