# Designer HTML Prototype Intake

The supplied `design-canvas-export.zip` contains 25 standalone HTML reference screens branded **Deliveram**. It is accepted as a visual and interaction starting point, not production source code and not an override of the SRS, UI/UX specification, security model, RBAC, accessibility requirements, or backend workflow.

## Supplied screen coverage

- Landing page
- Secure sign-in
- Profile and security
- Campaign dashboard
- Segment builder
- Audience repository
- Audience import upload and mapping
- Audience import validation
- Import processing and error report
- Audience snapshot review
- Campaign audience and eligibility
- Campaign composer
- Campaign messages
- Campaign pre-flight
- Campaign release step-up confirmation
- Live campaign operations
- Live campaign emergency controls
- Campaign emergency control
- Consent review queue
- Sender and worker operations
- Operations dashboard
- Organisation detail
- Campaign reports and audit
- Administration and audit
- Mobile approvals and alerts

## Production implementation rule

The eventual Next.js/TypeScript frontend may reuse layout ideas, interaction patterns and visual assets after review. Engineering has authority to redesign, consolidate, expand or replace screens so that the production interface correctly supports:

- the actual backend domain and lifecycle;
- exceptional, loading, empty, denied and failure states;
- maker-checker and MFA step-up;
- immutable evidence and optimistic concurrency;
- responsive behaviour and WCAG 2.2 AA accessibility;
- masking, privacy and role-specific visibility;
- server-side validation and authoritative data;
- the internal managed-service operating model.

The HTML package will be revisited after the operational backend contracts are substantially complete and stable.
