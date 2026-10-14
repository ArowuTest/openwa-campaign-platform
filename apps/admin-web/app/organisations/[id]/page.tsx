import { PageHeader } from '../../../components/page-header';
import { OrganisationWorkspace } from './organisation-workspace';

export default async function OrganisationDetailPage({
  params
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  return (
    <>
      <PageHeader
        title="Organisation workspace"
        description="Vetted client profile, governed policies, campaigns, consent reviews and audit evidence in one operator view."
      />
      <OrganisationWorkspace organisationId={id} />
    </>
  );
}
