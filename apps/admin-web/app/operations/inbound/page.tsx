import { PageHeader } from '../../../components/page-header';
import { InboundReviewWorkbench } from './inbound-review-workbench';

export default function InboundReviewPage() {
  return (
    <>
      <PageHeader
        title="Inbound replies and opt-outs"
        description="Review incoming WhatsApp replies without exposing message content by default. STOP suppressions take precedence over campaign convenience."
      />
      <InboundReviewWorkbench />
    </>
  );
}
