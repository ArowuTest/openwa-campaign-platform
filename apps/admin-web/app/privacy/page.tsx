import { PageHeader } from '../../components/page-header';
import { PrivacyCaseManager } from './privacy-case-manager';

export default function PrivacyPage() {
  return (
    <>
      <PageHeader
        title="Privacy cases"
        description="Manage masked data-subject requests with due dates, immutable history, maker-checker decisions and governed package exports."
      />
      <PrivacyCaseManager />
    </>
  );
}
