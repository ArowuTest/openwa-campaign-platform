import { OrganisationManager } from './organisation-manager';
import { PageHeader } from '../../components/page-header';

export default function OrganisationsPage() {
  return (
    <>
      <PageHeader
        title="Organisations"
        description="Internal records for organisations requesting managed campaigns. Organisations do not access this portal."
      />
      <OrganisationManager />
    </>
  );
}
