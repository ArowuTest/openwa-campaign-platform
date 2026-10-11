import { PageHeader } from '../../components/page-header';
import { ReportsConsole } from './reports-console';

export default function ReportsPage() {
  return (
    <>
      <PageHeader
        title="Reports"
        description="Inspect privacy-aware campaign, organisation and financial reconciliation snapshots without exposing raw recipient identities."
      />
      <ReportsConsole />
    </>
  );
}
