import 'reflect-metadata';
import { NestFactory } from '@nestjs/core';
import { AppModule } from './app.module';

async function bootstrap() {
  const app = await NestFactory.create(AppModule, {
    cors: false,
    bodyParser: true,
    rawBody: true
  });
  app.enableShutdownHooks();
  app.getHttpAdapter().getInstance().disable('x-powered-by');
  await app.listen(process.env.PORT ?? 2785, '0.0.0.0');
}

void bootstrap();
