'use client';

import Link from 'next/link';

import { defaultLandingPath } from '../../lib/navigation';
import { useAuth } from '../../components/auth-provider';

export default function AccessDeniedPage() {
  const { session } = useAuth();
  const home = defaultLandingPath(session?.permissions);
  return (
    <section className="state-page">
      <span className="state-code">403</span>
      <h1>Access not available</h1>
      <p>Your operator role does not include the permission required for this area. The control API remains authoritative even when a UI control is hidden.</p>
      <Link className="primary link-button" href={home}>Return to an authorised area</Link>
    </section>
  );
}
