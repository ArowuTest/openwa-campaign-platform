export const dynamic = 'force-dynamic';

export function GET(): Response {
  return Response.json(
    { status: 'ok', service: 'admin-web' },
    { headers: { 'cache-control': 'no-store' } }
  );
}
