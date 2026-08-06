# PostgreSQL service identities

Production and staging deployments use two layers of PostgreSQL identity:

1. Stable `NOLOGIN` privilege roles maintained by the repository:
   - `campaign_control_api`
   - `campaign_audience_worker`
   - `campaign_campaign_worker`
   - `campaign_export_worker`
   - `campaign_inbound_governance_worker`
   - `campaign_metrics_worker`
   - `campaign_platform_governance_worker`
2. Rotatable environment-specific `LOGIN` roles created by infrastructure provisioning. Each login role must be a member of exactly one privilege role.

For example, a rotated login such as `prod_control_api_202608` may be granted membership in `campaign_control_api`. The service-specific database URL contains the rotatable login credential, while `DATABASE_EXPECTED_ROLE` remains `campaign_control_api`.

`database/bootstrap/002_service_roles.sql` creates and reconciles the stable `NOLOGIN` privilege roles before and after migrations. It never creates or stores a password. Infrastructure provisioning must create the login role, set its secret outside the repository and grant only the intended service-role membership.

Every deployed Go process validates all of the following before becoming ready:

1. the PostgreSQL DSN uses `sslmode=require`, `verify-ca` or `verify-full`;
2. the connected login is not a PostgreSQL superuser;
3. the login is a member of the expected service privilege role; and
4. the login is not a member of any other campaign-platform service privilege role.

A mismatch fails startup. The owner/migration role must never be supplied to an application container.

Passwords and connection URLs must be delivered through `DATABASE_URL_FILE` or an external secret mount. Rotation is performed by creating a replacement login role with membership in the same privilege role, updating the mounted database URL, restarting one replica at a time, and revoking/dropping the old login only after all replicas have reconnected.
