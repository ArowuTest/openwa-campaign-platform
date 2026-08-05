export function StatusBadge({ status }: { status: string }) {
  const normalized = status.toLowerCase().replaceAll('_', '-');
  return <span className={`status status-${normalized}`}>{status.replaceAll('_', ' ')}</span>;
}
