import { PageHeader } from '../../components/page-header';
import { LegalHoldManager } from './legal-hold-manager';
import { PrivacyCaseManager } from './privacy-case-manager';

export default function PrivacyPage() {
  return (
    <>
      <PageHeader
        title="Privacy cases"
        description="Manage masked data-subject requests, legal holds, due dates, immutable history, maker-checker decisions and governed package exports."
      />
      <PrivacyCaseManager />
      <LegalHoldManager />
    </>
  );
}
