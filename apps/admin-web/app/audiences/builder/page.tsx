import { FilterBuilder } from '../../../components/filter-builder';

export default function AudienceBuilderPage() {
  return (
    <>
      <div className="toolbar">
        <div>
          <h1>Audience builder</h1>
          <p className="muted">Create reusable segments using governed demographic, geography, consent and engagement filters.</p>
        </div>
        <button className="primary" type="button">Save segment</button>
      </div>
      <FilterBuilder />
    </>
  );
}
