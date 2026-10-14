import { PageHeader } from '../../components/page-header';
import { WorkApprovalBoard } from './work-approval-board';

export default function WorkPage() {
  return (
    <>
      <PageHeader
        title="My work and approvals"
        description="Find pending consent, commercial, message and final-campaign reviews. Every decision remains in its governed source workspace."
      />
      <WorkApprovalBoard />
    </>
  );
}
