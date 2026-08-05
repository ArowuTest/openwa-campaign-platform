const internalBaseURL = process.env.CONTROL_API_INTERNAL_URL ?? 'http://localhost:8080';

export async function serverAPI<T>(path: string): Promise<T> {
  const response = await fetch(`${internalBaseURL}${path}`, {
    cache: 'no-store',
    headers: { Accept: 'application/json' }
  });
  if (!response.ok) {
    throw new Error(`Control API request failed: ${response.status}`);
  }
  return response.json() as Promise<T>;
}

export const browserAPIBase = process.env.NEXT_PUBLIC_CONTROL_API_BASE ?? '/api';
