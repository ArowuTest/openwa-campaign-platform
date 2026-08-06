import 'reflect-metadata';
import { NestFactory } from '@nestjs/core';
import { resolveSecretFiles } from './secret-file';
import { AppModule } from './app.module';
import { SanitisedJsonLogger } from './observability.service';

async function bootstrap() {
  resolveSecretFiles([
    'GATEWAY_COMMAND_SECRET','GATEWAY_COMMAND_SECRET_PREVIOUS','GATEWAY_CALLBACK_SECRET',
    'GATEWAY_CALLBACK_SECRET_PREVIOUS','GATEWAY_RUNTIME_SECRET','OPENWA_UPSTREAM_API_KEY',
    'OPENWA_WEBHOOK_SECRET','MEDIA_DOWNLOAD_SECRET','PROFILING_TOKEN'
  ]);
  const app = await NestFactory.create(AppModule, {
    cors: false,
    bodyParser: true,
    rawBody: true
  });
  app.useLogger(new SanitisedJsonLogger());
  app.enableShutdownHooks();
  app.getHttpAdapter().getInstance().disable('x-powered-by');
  await app.listen(process.env.PORT ?? 2785, '0.0.0.0');
}

void bootstrap();
