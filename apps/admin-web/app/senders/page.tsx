import { PageHeader } from '../../components/page-header';
import { SenderConsole } from './sender-console';

export default function SendersPage() {
  return (
    <>
      <PageHeader
        title="Senders"
        description="Govern OpenWA pools, sessions, nodes and approved test recipients. Sensitive session material is never displayed in inventory views."
      />
      <SenderConsole />
    </>
  );
}
