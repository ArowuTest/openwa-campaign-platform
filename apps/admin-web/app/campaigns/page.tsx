import { PageHeader } from '../../components/page-header';
import { CampaignManager } from './campaign-manager';

export default function CampaignsPage() {
  return (
    <>
      <PageHeader
        title="Campaigns"
        description="Create internally managed campaigns and move them through controlled consent, audience, message, commercial and final approvals."
      />
      <CampaignManager />
    </>
  );
}
