export async function boundedResponseDetail(response: Response, maximumBytes = 300): Promise<string> {
  if (!Number.isInteger(maximumBytes) || maximumBytes < 1 || maximumBytes > 4096) {
    throw new TypeError('control-plane response detail limit is invalid');
  }
  const reader = response.body?.getReader();
  if (!reader) return '';
  const decoder = new TextDecoder();
  let remaining = maximumBytes;
  let detail = '';
  try {
    while (remaining > 0) {
      const { done, value } = await reader.read();
      if (done) break;
      if (!value?.byteLength) continue;
      const take = value.byteLength > remaining ? value.subarray(0, remaining) : value;
      detail += decoder.decode(take, { stream: true });
      remaining -= take.byteLength;
      if (value.byteLength > take.byteLength || remaining === 0) {
        await reader.cancel().catch(() => undefined);
        break;
      }
    }
    detail += decoder.decode();
  } finally {
    reader.releaseLock();
  }
  return detail.replace(/[\r\n\t\u0000-\u001f\u007f]+/gu, ' ').slice(0, maximumBytes);
}
