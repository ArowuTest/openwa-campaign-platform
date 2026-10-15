import { OrganisationManager } from './organisation-manager';
import { PageHeader } from '../../components/page-header';

export default async function OrganisationsPage({
  searchParams
}: {
  searchParams: Promise<{ organisationId?: string | string[] }>;
}) {
  const params = await searchParams;
  const organisationId = Array.isArray(params.organisationId)
    ? params.organisationId[0]
    : params.organisationId;
  return (
    <>
      <PageHeader
        title="Organisations"
        description="Internal records for organisations requesting managed campaigns. Organisations do not access this portal."
      />
      <OrganisationManager initialOrganisationId={organisationId} />
    </>
  );
}
