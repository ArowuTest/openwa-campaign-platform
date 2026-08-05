import { PageHeader } from '../../components/page-header';
import { ConsentReviewManager } from './review-manager';

export default function ConsentReviewsPage() {
  return (
    <>
      <PageHeader
        title="Consent reviews"
        description="Record the offline assessment of consent wording, evidence, permitted use and expiry before any audience import or campaign."
      />
      <ConsentReviewManager />
    </>
  );
}
