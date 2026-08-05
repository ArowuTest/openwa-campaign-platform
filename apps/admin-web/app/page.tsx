export default function DashboardPage() {
  return (
    <>
      <div className="toolbar">
        <div>
          <h1>Operations dashboard</h1>
          <p className="muted">Truthful campaign, queue and sender status for authorised internal users.</p>
        </div>
        <button className="primary" type="button">Create campaign</button>
      </div>
      <section className="grid grid-4" aria-label="Operational metrics">
        {[
          ['Active campaigns', '0'],
          ['Eligible audience profiles', '0'],
          ['Messages queued', '0'],
          ['Healthy sender sessions', '0']
        ].map(([label, value]) => (
          <article className="card metric" key={label}><span className="muted">{label}</span><strong>{value}</strong></article>
        ))}
      </section>
    </>
  );
}
