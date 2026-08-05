import { PageHeader } from '../../../components/page-header';
import { ImportPreview } from './import-preview';

export default function AudienceImportsPage() {
  return (
    <>
      <PageHeader
        title="Audience imports"
        description="Preview and validate an approved consented-audience file before any permanent contact or consent records are written."
      />
      <ImportPreview />
    </>
  );
}
