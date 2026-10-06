import { PageHeader } from '../../../components/page-header';
import { CampaignWorkspace } from './campaign-workspace';

export default async function CampaignWorkspacePage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return (
    <>
      <PageHeader
        title="Campaign workspace"
        description="Prepare evidence, run controlled tests, reserve the exact OpenWA route and execute only through governed server-side gates."
      />
      <CampaignWorkspace campaignId={id} />
    </>
  );
}
