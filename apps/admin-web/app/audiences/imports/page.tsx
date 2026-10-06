import { PageHeader } from '../../../components/page-header';
import { AudienceImportWorkspace } from './import-workspace';

export default function AudienceImportsPage() {
  return (
    <>
      <PageHeader
        title="Audience imports"
        description="Upload, map, validate, approve and reconcile governed audience sources with durable progress and recoverable long-running processing."
      />
      <AudienceImportWorkspace />
    </>
  );
}
