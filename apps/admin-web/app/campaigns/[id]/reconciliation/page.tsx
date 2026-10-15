import { PageHeader } from '../../../../components/page-header';
import { CompletionReconciliationPanel } from './completion-reconciliation-panel';

export default async function ReconciliationPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return (
    <>
      <PageHeader
        title="Campaign completion and reconciliation"
        description="Review delivery obligations, exceptions and financial evidence before a governed campaign close-out."
      />
      <CompletionReconciliationPanel campaignId={id} />
    </>
  );
}
