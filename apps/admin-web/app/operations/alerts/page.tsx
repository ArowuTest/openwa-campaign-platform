import { PageHeader } from '../../../components/page-header';
import { AlertCentre } from './alert-centre';

export default function AlertsPage() {
  return (
    <>
      <PageHeader
        title="Alert Centre"
        description="Triage persistent platform alerts, inspect their immutable event history and acknowledge incidents with attributable reasons."
      />
      <AlertCentre />
    </>
  );
}
