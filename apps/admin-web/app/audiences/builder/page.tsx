import { FilterBuilder } from '../../../components/filter-builder';

export default function AudienceBuilderPage() {
  return (
    <>
      <div className="toolbar">
        <div>
          <h1>Audience builder</h1>
          <p className="muted">Build, validate, estimate and save reusable segments using the current governed filter registry.</p>
        </div>
      </div>
      <FilterBuilder />
    </>
  );
}
