import { PageHeader } from '../../../../components/page-header';
import { FinanceApprovalDetail } from './finance-approval-detail';

export default async function FinanceApprovalPage({
  params
}: {
  params: Promise<{ campaignId: string }>;
}) {
  const { campaignId } = await params;
  return (
    <>
      <PageHeader
        title="Commercial evidence review"
        description="Independent finance evidence decisions without requiring unrelated campaign-read privileges."
      />
      <FinanceApprovalDetail campaignId={campaignId} />
    </>
  );
}
