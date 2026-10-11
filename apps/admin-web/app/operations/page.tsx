import { PageHeader } from '../../components/page-header';
import { OperationsConsole } from './operations-console';

export default function OperationsPage() {
  return (
    <>
      <PageHeader
        title="Operations"
        description="Monitor queue health, incidents and delivery exceptions. UNKNOWN outcomes remain sticky until reviewed evidence resolves them."
      />
      <OperationsConsole />
    </>
  );
}
