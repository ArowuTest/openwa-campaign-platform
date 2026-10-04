package postgres

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"campaign-platform/internal/identity"
	sharedcrypto "campaign-platform/internal/shared/crypto"
)

type IdentityRepository struct {
	DB      *sql.DB
	Secrets *sharedcrypto.SecretBox
}

func (r *IdentityRepository) ByEmail(ctx context.Context, email string) (identity.User, error) {
	return r.scanUser(r.DB.QueryRowContext(ctx, identityQuery("lower(u.email::text)=lower($1)"), strings.TrimSpace(email)))
}
func (r *IdentityRepository) ByID(ctx context.Context, id string) (identity.User, error) {
	return r.scanUser(r.DB.QueryRowContext(ctx, identityQuery("u.id=$1::uuid"), strings.TrimSpace(id)))
}

const identitySelect = `
SELECT u.id::text,u.email::text,u.display_name,u.status,u.mfa_required,
       coalesce(c.password_hash,''),c.totp_secret_ciphertext,c.failed_login_count,c.locked_until,u.last_login_at,
       coalesce(array_agg(DISTINCT p.permission) FILTER (WHERE p.permission IS NOT NULL),'{}'::text[])
FROM internal_users u
JOIN internal_user_credentials c ON c.user_id=u.id
LEFT JOIN user_roles ur ON ur.user_id=u.id
LEFT JOIN roles r ON r.id=ur.role_id
LEFT JOIN LATERAL unnest(r.permissions) p(permission) ON true`

const identityGroupBy = `
GROUP BY u.id,u.email,u.display_name,u.status,u.mfa_required,c.password_hash,c.totp_secret_ciphertext,c.failed_login_count,c.locked_until,u.last_login_at`

func identityQuery(predicate string) string {
	return identitySelect + "\nWHERE " + predicate + identityGroupBy
}

func (r *IdentityRepository) scanUser(row scanner) (identity.User, error) {
	if r == nil || r.DB == nil || r.Secrets == nil {
		return identity.User{}, errors.New("persistent identity repository is not configured")
	}
	var u identity.User
	var status string
	var encrypted []byte
	var locked, last sql.NullTime
	var permissions []string
	err := row.Scan(&u.ID, &u.Email, &u.DisplayName, &status, &u.MFARequired, &u.PasswordHash, &encrypted, &u.FailedLoginCount, &locked, &last, &permissions)
	if errors.Is(err, sql.ErrNoRows) {
		return identity.User{}, identity.ErrUserNotFound
	}
	if err != nil {
		return identity.User{}, err
	}
	u.Status = identity.Status(status)
	u.Permissions = map[string]struct{}{}
	for _, p := range permissions {
		u.Permissions[p] = struct{}{}
	}
	if len(encrypted) > 0 {
		secret, err := r.Secrets.Open("identity:totp:"+u.ID, encrypted)
		if err != nil {
			return identity.User{}, fmt.Errorf("decrypt TOTP secret: %w", err)
		}
		u.TOTPSecret = secret
	}
	if locked.Valid {
		v := locked.Time.UTC()
		u.LockedUntil = &v
	}
	if last.Valid {
		v := last.Time.UTC()
		u.LastLoginAt = &v
	}
	return u, nil
}

func (r *IdentityRepository) Save(ctx context.Context, u identity.User) error {
	if r == nil || r.DB == nil || r.Secrets == nil {
		return errors.New("persistent identity repository is not configured")
	}
	secret, err := r.Secrets.Seal("identity:totp:"+u.ID, u.TOTPSecret)
	if err != nil {
		return err
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE internal_users SET email=$2,display_name=$3,status=$4,mfa_required=$5,last_login_at=$6,updated_at=now(),version=version+1 WHERE id=$1::uuid`, u.ID, strings.ToLower(strings.TrimSpace(u.Email)), u.DisplayName, u.Status, u.MFARequired, u.LastLoginAt)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return identity.ErrUserNotFound
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO internal_user_credentials(user_id,password_hash,totp_secret_ciphertext,failed_login_count,locked_until,updated_at) VALUES($1::uuid,$2,$3,$4,$5,now()) ON CONFLICT(user_id) DO UPDATE SET password_hash=excluded.password_hash,totp_secret_ciphertext=excluded.totp_secret_ciphertext,failed_login_count=excluded.failed_login_count,locked_until=excluded.locked_until,updated_at=now()`, u.ID, u.PasswordHash, secret, u.FailedLoginCount, u.LockedUntil)
	if err != nil {
		return err
	}
	return tx.Commit()
}

type bootstrapIdentityRecord struct {
	ID    string
	Email string
}

func resolveBootstrapIdentity(requestedID, requestedEmail string, matches []bootstrapIdentityRecord) (string, bool, error) {
	requestedID = strings.TrimSpace(requestedID)
	requestedEmail = strings.ToLower(strings.TrimSpace(requestedEmail))

	var idMatch, emailMatch *bootstrapIdentityRecord
	for index := range matches {
		match := &matches[index]
		match.ID = strings.TrimSpace(match.ID)
		match.Email = strings.ToLower(strings.TrimSpace(match.Email))
		if requestedID != "" && match.ID == requestedID {
			idMatch = match
		}
		if requestedEmail != "" && match.Email == requestedEmail {
			emailMatch = match
		}
	}
	if idMatch == nil && emailMatch == nil {
		return "", true, nil
	}
	if idMatch != nil && emailMatch != nil && idMatch.ID == emailMatch.ID && idMatch.Email == emailMatch.Email {
		return idMatch.ID, false, nil
	}
	return "", false, fmt.Errorf("bootstrap identity collision: requested ID %q and email %q do not identify the same user", requestedID, requestedEmail)
}

func (r *IdentityRepository) EnsureBootstrapAdministrator(ctx context.Context, u identity.User) (identity.User, bool, error) {
	if r == nil || r.DB == nil || r.Secrets == nil {
		return identity.User{}, false, errors.New("persistent identity repository is not configured")
	}
	requestedID := strings.TrimSpace(u.ID)
	normalizedEmail := strings.ToLower(strings.TrimSpace(u.Email))

	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return identity.User{}, false, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
SELECT id::text,email::text
FROM internal_users
WHERE id=$1::uuid OR lower(email::text)=lower($2)
FOR UPDATE`, requestedID, normalizedEmail)
	if err != nil {
		return identity.User{}, false, err
	}
	matches := make([]bootstrapIdentityRecord, 0, 2)
	for rows.Next() {
		var match bootstrapIdentityRecord
		if err := rows.Scan(&match.ID, &match.Email); err != nil {
			_ = rows.Close()
			return identity.User{}, false, err
		}
		matches = append(matches, match)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return identity.User{}, false, err
	}
	if err := rows.Close(); err != nil {
		return identity.User{}, false, err
	}

	existingID, create, err := resolveBootstrapIdentity(requestedID, normalizedEmail, matches)
	if err != nil {
		return identity.User{}, false, err
	}
	if !create {
		if err := tx.Commit(); err != nil {
			return identity.User{}, false, err
		}
		existing, err := r.ByID(ctx, existingID)
		if err != nil {
			return identity.User{}, false, fmt.Errorf("load existing bootstrap administrator %q: %w", existingID, err)
		}
		return existing, false, nil
	}

	secret, err := r.Secrets.Seal("identity:totp:"+requestedID, u.TOTPSecret)
	if err != nil {
		return identity.User{}, false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,$3,'ACTIVE',$4)`, requestedID, normalizedEmail, u.DisplayName, u.MFARequired)
	if err != nil {
		return identity.User{}, false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO internal_user_credentials(user_id,password_hash,totp_secret_ciphertext) VALUES($1::uuid,$2,$3)`, requestedID, u.PasswordHash, secret)
	if err != nil {
		return identity.User{}, false, err
	}
	roleResult, err := tx.ExecContext(ctx, `INSERT INTO user_roles(user_id,role_id,assigned_by) SELECT $1::uuid,id,$1::uuid FROM roles WHERE code='SUPER_ADMIN'`, requestedID)
	if err != nil {
		return identity.User{}, false, err
	}
	roleCount, err := roleResult.RowsAffected()
	if err != nil {
		return identity.User{}, false, err
	}
	if roleCount != 1 {
		return identity.User{}, false, errors.New("SUPER_ADMIN role is not available for bootstrap administrator")
	}
	if err = tx.Commit(); err != nil {
		return identity.User{}, false, err
	}
	created, err := r.ByID(ctx, requestedID)
	return created, true, err
}

type IdentitySessionRepository struct{ DB *sql.DB }

func (r *IdentitySessionRepository) Create(ctx context.Context, h [32]byte, s identity.Session) error {
	_, err := r.DB.ExecContext(ctx, `INSERT INTO internal_user_sessions(id,user_id,token_hash,csrf_hash,created_at,last_seen_at,idle_expires_at,absolute_expires_at,mfa_verified_at,source_ip,user_agent) VALUES($1::uuid,$2::uuid,$3,$4,$5,$6,$7,$8,$9,NULLIF($10,'')::inet,NULLIF($11,''))`, s.ID, s.UserID, h[:], s.CSRFHash, s.CreatedAt, s.LastSeenAt, s.ExpiresAt, s.AbsoluteAt, s.MFAVerifiedAt, s.SourceIP, s.UserAgent)
	return err
}
func (r *IdentitySessionRepository) Get(ctx context.Context, h [32]byte) (identity.Session, error) {
	return scanSession(r.DB.QueryRowContext(ctx, sessionSelect+` WHERE token_hash=$1 AND revoked_at IS NULL`, h[:]))
}
func (r *IdentitySessionRepository) Touch(ctx context.Context, h [32]byte, seen, expires time.Time) (identity.Session, error) {
	return scanSession(r.DB.QueryRowContext(ctx, `UPDATE internal_user_sessions SET last_seen_at=$2,idle_expires_at=$3 WHERE token_hash=$1 AND revoked_at IS NULL AND idle_expires_at>$2 AND absolute_expires_at>$2 RETURNING id::text,user_id::text,csrf_hash,created_at,last_seen_at,idle_expires_at,absolute_expires_at,mfa_verified_at,coalesce(source_ip::text,''),coalesce(user_agent,'')`, h[:], seen, expires))
}
func (r *IdentitySessionRepository) Revoke(ctx context.Context, h [32]byte, at time.Time, reason string) error {
	_, err := r.DB.ExecContext(ctx, `UPDATE internal_user_sessions SET revoked_at=coalesce(revoked_at,$2),revoke_reason=coalesce(revoke_reason,NULLIF($3,'')) WHERE token_hash=$1`, h[:], at, reason)
	return err
}
func (r *IdentitySessionRepository) RevokeAll(ctx context.Context, userID string, at time.Time, reason string) (int, error) {
	res, err := r.DB.ExecContext(ctx, `UPDATE internal_user_sessions SET revoked_at=$2,revoke_reason=NULLIF($3,'') WHERE user_id=$1::uuid AND revoked_at IS NULL`, userID, at, reason)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}
func (r *IdentitySessionRepository) ActiveByUser(ctx context.Context, userID string, now time.Time) ([]identity.Session, error) {
	rows, err := r.DB.QueryContext(ctx, sessionSelect+` WHERE user_id=$1::uuid AND revoked_at IS NULL AND idle_expires_at>$2 AND absolute_expires_at>$2 ORDER BY created_at DESC`, userID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []identity.Session{}
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
func (r *IdentitySessionRepository) MarkMFAVerified(ctx context.Context, h [32]byte, at time.Time) (identity.Session, error) {
	return scanSession(r.DB.QueryRowContext(ctx, `UPDATE internal_user_sessions SET mfa_verified_at=$2 WHERE token_hash=$1 AND revoked_at IS NULL AND idle_expires_at>$2 AND absolute_expires_at>$2 RETURNING id::text,user_id::text,csrf_hash,created_at,last_seen_at,idle_expires_at,absolute_expires_at,mfa_verified_at,coalesce(source_ip::text,''),coalesce(user_agent,'')`, h[:], at))
}

const sessionSelect = `SELECT id::text,user_id::text,csrf_hash,created_at,last_seen_at,idle_expires_at,absolute_expires_at,mfa_verified_at,coalesce(source_ip::text,''),coalesce(user_agent,'') FROM internal_user_sessions`

func scanSession(row scanner) (identity.Session, error) {
	var s identity.Session
	var mfa sql.NullTime
	err := row.Scan(&s.ID, &s.UserID, &s.CSRFHash, &s.CreatedAt, &s.LastSeenAt, &s.ExpiresAt, &s.AbsoluteAt, &mfa, &s.SourceIP, &s.UserAgent)
	if errors.Is(err, sql.ErrNoRows) {
		return identity.Session{}, identity.ErrSessionNotFound
	}
	if mfa.Valid {
		v := mfa.Time.UTC()
		s.MFAVerifiedAt = &v
	}
	return s, err
}

type IdentityChallengeRepository struct{ DB *sql.DB }

func (r *IdentityChallengeRepository) Create(ctx context.Context, h [32]byte, c identity.Challenge) error {
	_, err := r.DB.ExecContext(ctx, `INSERT INTO internal_mfa_challenges(token_hash,user_id,expires_at) VALUES($1,$2::uuid,$3)`, h[:], c.UserID, c.ExpiresAt)
	return err
}
func (r *IdentityChallengeRepository) Consume(ctx context.Context, h [32]byte, now time.Time) (identity.Challenge, error) {
	var c identity.Challenge
	var used sql.NullTime
	err := r.DB.QueryRowContext(ctx, `UPDATE internal_mfa_challenges SET used_at=$2 WHERE token_hash=$1 AND used_at IS NULL AND expires_at>$2 RETURNING user_id::text,expires_at,used_at`, h[:], now).Scan(&c.UserID, &c.ExpiresAt, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return identity.Challenge{}, identity.ErrInvalidMFA
	}
	if used.Valid {
		v := used.Time.UTC()
		c.UsedAt = &v
	}
	return c, err
}

func (r *IdentityRepository) RecordFailedLogin(ctx context.Context, userID string, now time.Time, threshold int, lockDuration time.Duration) error {
	if threshold <= 0 {
		threshold = 5
	}
	if lockDuration <= 0 {
		lockDuration = 15 * time.Minute
	}
	res, err := r.DB.ExecContext(ctx, `UPDATE internal_user_credentials
SET failed_login_count=failed_login_count+1,
    locked_until=CASE WHEN failed_login_count+1 >= $3 THEN $2::timestamptz + $4::interval ELSE locked_until END,
    updated_at=$2
WHERE user_id=$1::uuid`, userID, now.UTC(), threshold, intervalLiteral(lockDuration))
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return identity.ErrUserNotFound
	}
	return nil
}

func (r *IdentityRepository) RecordSuccessfulLogin(ctx context.Context, userID string, now time.Time) error {
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE internal_users SET last_login_at=$2,updated_at=$2 WHERE id=$1::uuid`, userID, now.UTC())
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return identity.ErrUserNotFound
	}
	if _, err = tx.ExecContext(ctx, `UPDATE internal_user_credentials SET failed_login_count=0,locked_until=NULL,updated_at=$2 WHERE user_id=$1::uuid`, userID, now.UTC()); err != nil {
		return err
	}
	return tx.Commit()
}

func intervalLiteral(value time.Duration) string {
	seconds := int64(value / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	return fmt.Sprintf("%d seconds", seconds)
}

type AuthenticationEventRecorder struct{ DB *sql.DB }

func (r *AuthenticationEventRecorder) Append(ctx context.Context, e identity.AuthenticationEvent) error {
	if r == nil || r.DB == nil {
		return errors.New("database is required")
	}
	encodedMetadata, err := json.Marshal(e.Metadata)
	if err != nil {
		return fmt.Errorf("encode authentication metadata: %w", err)
	}
	metadata := string(encodedMetadata)
	var emailHash []byte
	if e.EmailHash != "" {
		decoded, err := hex.DecodeString(e.EmailHash)
		if err != nil {
			return err
		}
		emailHash = decoded
	}
	_, err = r.DB.ExecContext(ctx, `INSERT INTO authentication_events(id,user_id,email_hash,event_type,source_ip,user_agent,correlation_id,occurred_at,metadata)
VALUES($1::uuid,NULLIF($2,'')::uuid,$3,$4,NULLIF($5,'')::inet,NULLIF($6,''),$7,$8,$9::jsonb)`, e.ID, e.UserID, emailHash, e.Type, e.SourceIP, e.UserAgent, e.CorrelationID, e.OccurredAt, metadata)
	return err
}
