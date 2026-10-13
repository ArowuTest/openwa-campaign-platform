import { PageHeader } from '../../../components/page-header';
import { UserManager } from './user-manager';

export default function UsersPage() {
  return (
    <>
      <PageHeader
        title="Users"
        description="Provision named internal operators, maintain role assignments and MFA policy, and revoke access with attributable reasons."
      />
      <UserManager />
    </>
  );
}
