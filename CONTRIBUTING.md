# Contributing

- Use short-lived branches.
- Never commit secrets, MSISDNs, consent evidence or production exports.
- Keep Go business logic out of Next.js and OpenWA gateway code.
- Database changes require forward migration, rollback strategy and data-impact notes.
- New cohort filters require a governed definition, permission assessment, query plan and test.
- Gateway acceptance must never be labelled delivery.
- Run `make check` before review.
