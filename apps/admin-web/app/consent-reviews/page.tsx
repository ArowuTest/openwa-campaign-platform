import { PageHeader } from '../../components/page-header';
import { ConsentReviewManager } from './review-manager';

export default async function ConsentReviewsPage({
  searchParams
}: {
  searchParams: Promise<{ reviewId?: string | string[] }>;
}) {
  const params = await searchParams;
  const reviewId = Array.isArray(params.reviewId) ? params.reviewId[0] : params.reviewId;
  return (
    <>
      <PageHeader
        title="Consent reviews"
        description="Record the offline assessment of consent wording, evidence, permitted use and expiry before any audience import or campaign."
      />
      <ConsentReviewManager initialReviewId={reviewId} />
    </>
  );
}
