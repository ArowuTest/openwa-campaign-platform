import { ControlledExportManager } from '../../components/controlled-export-manager';
import { PageHeader } from '../../components/page-header';
import { AuditConsole } from './audit-console';

export default function AuditPage() {
  return (
    <>
      <PageHeader
        title="Audit"
        description="Search immutable governance evidence with compliance-reader access. Audit queries are themselves recorded."
      />
      <AuditConsole />
      <ControlledExportManager kind="AUDIT_LOG" title="Controlled audit exports" />
    </>
  );
}
