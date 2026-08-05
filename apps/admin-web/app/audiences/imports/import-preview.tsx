'use client';

import { FormEvent, useState } from 'react';

type Preview = {
  uploadedRows: number;
  validRows: number;
  invalidRows: number;
  duplicateRows: number;
  issues: Array<{ rowNumber: number; field: string; code: string; message: string }>;
};

export function ImportPreview() {
  const [preview, setPreview] = useState<Preview | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setLoading(true);
    setError('');
    setPreview(null);
    const body = new FormData(event.currentTarget);
    const response = await fetch('/api/v1/audience-imports/preview', { method: 'POST', body });
    const result = await response.json().catch(() => ({}));
    if (!response.ok) setError(result.message ?? 'Import preview failed');
    else setPreview(result);
    setLoading(false);
  }

  return (
    <div className="split-layout">
      <form className="card form-stack" onSubmit={submit}>
        <h2>Upload CSV for preview</h2>
        <div className="alert alert-information">Preview does not persist raw MSISDNs. Values are normalised and only aggregate outcomes and row issues are returned.</div>
        <label>CSV file<input type="file" name="file" accept=".csv,text/csv" required /></label>
        <label>Default country
          <select name="defaultCountry" defaultValue="NG"><option value="NG">Nigeria</option><option value="GH">Ghana</option><option value="GB">United Kingdom</option></select>
        </label>
        <div className="grid grid-2">
          <label>MSISDN column<input name="msisdnColumn" defaultValue="msisdn" required /></label>
          <label>Country column<input name="countryColumn" defaultValue="country" /></label>
          <label>State column<input name="stateColumn" defaultValue="state" /></label>
          <label>LGA column<input name="lgaColumn" defaultValue="lga" /></label>
          <label>Age column<input name="ageColumn" defaultValue="age" /></label>
          <label>Gender column<input name="genderColumn" defaultValue="gender" /></label>
        </div>
        <button className="primary" disabled={loading}>{loading ? 'Validating…' : 'Preview import'}</button>
        {error ? <div className="alert alert-danger">{error}</div> : null}
      </form>

      <section className="card">
        <h2>Validation outcome</h2>
        {!preview ? <div className="empty-state"><strong>No preview generated</strong><p>Upload a CSV to see valid, invalid and duplicate row counts.</p></div> : (
          <>
            <div className="grid grid-4">
              <div className="metric-tile"><span>Uploaded</span><strong>{preview.uploadedRows.toLocaleString()}</strong></div>
              <div className="metric-tile success"><span>Valid</span><strong>{preview.validRows.toLocaleString()}</strong></div>
              <div className="metric-tile danger"><span>Invalid</span><strong>{preview.invalidRows.toLocaleString()}</strong></div>
              <div className="metric-tile warning"><span>Duplicates</span><strong>{preview.duplicateRows.toLocaleString()}</strong></div>
            </div>
            {preview.issues.length ? (
              <div className="table-wrap">
                <table><thead><tr><th>Row</th><th>Field</th><th>Issue</th><th>Explanation</th></tr></thead>
                <tbody>{preview.issues.slice(0, 100).map((issue, index) => <tr key={`${issue.rowNumber}-${index}`}><td>{issue.rowNumber}</td><td>{issue.field || '—'}</td><td>{issue.code}</td><td>{issue.message}</td></tr>)}</tbody>
                </table>
              </div>
            ) : <div className="alert alert-success">No row-level validation issues were found.</div>}
          </>
        )}
      </section>
    </div>
  );
}
