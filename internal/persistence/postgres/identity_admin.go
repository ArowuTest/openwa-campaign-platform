package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"campaign-platform/internal/identity"
	sharedcrypto "campaign-platform/internal/shared/crypto"
)

type IdentityAdministrationRepository struct {
	DB      *sql.DB
	Secrets *sharedcrypto.SecretBox
}

const accountSelect = `SELECT u.id::text,u.email::text,u.display_name,u.status,u.mfa_required,u.version,u.last_login_at,u.created_at,u.updated_at,coalesce(array_agg(r.code ORDER BY r.code) FILTER(WHERE r.code IS NOT NULL),'{}'::text[]) FROM internal_users u LEFT JOIN user_roles ur ON ur.user_id=u.id LEFT JOIN roles r ON r.id=ur.role_id`

func (r *IdentityAdministrationRepository) ListAccounts(ctx context.Context) ([]identity.Account, error) {
	rows, err := r.DB.QueryContext(ctx, accountSelect+` GROUP BY u.id ORDER BY u.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []identity.Account{}
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (r *IdentityAdministrationRepository) ListAccountPage(ctx context.Context, limit int, before *time.Time, beforeID string) ([]identity.Account, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("identity administration repository is not configured")
	}
	if limit <= 0 || limit > 501 {
		limit = 100
	}
	rows, err := r.DB.QueryContext(ctx, accountSelect+` WHERE ($2::timestamptz IS NULL OR u.created_at<$2 OR (u.created_at=$2 AND u.id<NULLIF($3::text,'')::uuid)) GROUP BY u.id ORDER BY u.created_at DESC,u.id DESC LIMIT $1`, limit, before, beforeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]identity.Account, 0, limit)
	for rows.Next() {
		account, scanErr := scanAccount(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, account)
	}
	return out, rows.Err()
}

func (r *IdentityAdministrationRepository) GetAccount(ctx context.Context, id string) (identity.Account, error) {
	a, err := scanAccount(r.DB.QueryRowContext(ctx, accountSelect+` WHERE u.id=$1::uuid GROUP BY u.id`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return identity.Account{}, identity.ErrAccountNotFound
	}
	return a, err
}
func scanAccount(row scanner) (identity.Account, error) {
	var a identity.Account
	var status string
	var last sql.NullTime
	err := row.Scan(&a.ID, &a.Email, &a.DisplayName, &status, &a.MFARequired, &a.Version, &last, &a.CreatedAt, &a.UpdatedAt, &a.RoleCodes)
	a.Status = identity.Status(status)
	if last.Valid {
		v := last.Time.UTC()
		a.LastLoginAt = &v
	}
	return a, err
}

func (r *IdentityAdministrationRepository) CreateAccount(ctx context.Context, a identity.Account, passwordHash, totp, actor, reason string) error {
	if r == nil || r.DB == nil || r.Secrets == nil {
		return errors.New("identity administration repository is not configured")
	}
	secret, err := r.Secrets.Seal("identity:totp:"+a.ID, totp)
	if err != nil {
		return err
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = validateRoleCodesTx(ctx, tx, a.RoleCodes); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required,version,created_at,updated_at,created_by,updated_by) VALUES($1::uuid,$2,$3,$4,$5,$6,$7,$7,$8::uuid,$8::uuid)`, a.ID, a.Email, a.DisplayName, a.Status, a.MFARequired, a.Version, a.CreatedAt, actor)
	if isUniqueViolation(err) {
		return identity.ErrAccountDuplicate
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO internal_user_credentials(user_id,password_hash,totp_secret_ciphertext) VALUES($1::uuid,$2,$3)`, a.ID, passwordHash, secret)
	if err != nil {
		return err
	}
	if err = replaceRolesTx(ctx, tx, a.ID, a.RoleCodes, actor); err != nil {
		return err
	}
	if err = insertAccountHistory(ctx, tx, a, actor, reason); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *IdentityAdministrationRepository) CompareAndSwapAccount(ctx context.Context, a identity.Account, expected int64, actor, reason string) error {
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var lockResult any
	if err = tx.QueryRowContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('identity-super-admin'))`).Scan(&lockResult); err != nil {
		return err
	}
	current, err := getAccountTx(ctx, tx, a.ID)
	if err != nil {
		return err
	}
	if current.Version != expected || a.Version != expected+1 {
		return identity.ErrAccountConflict
	}
	if err = validateRoleCodesTx(ctx, tx, a.RoleCodes); err != nil {
		return err
	}
	if hasRoleCode(current.RoleCodes, "SUPER_ADMIN") && (!hasRoleCode(a.RoleCodes, "SUPER_ADMIN") || a.Status != identity.StatusActive) {
		var others int
		err = tx.QueryRowContext(ctx, `SELECT count(*) FROM internal_users u JOIN user_roles ur ON ur.user_id=u.id JOIN roles r ON r.id=ur.role_id WHERE r.code='SUPER_ADMIN' AND u.status='ACTIVE' AND u.id<>$1::uuid`, a.ID).Scan(&others)
		if err != nil {
			return err
		}
		if others == 0 {
			return identity.ErrLastSuperAdmin
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE internal_users SET display_name=$3,status=$4,mfa_required=$5,version=$6,updated_at=$7,updated_by=$8::uuid WHERE id=$1::uuid AND version=$2`, a.ID, expected, a.DisplayName, a.Status, a.MFARequired, a.Version, a.UpdatedAt, actor)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return identity.ErrAccountConflict
	}
	if err = replaceRolesTx(ctx, tx, a.ID, a.RoleCodes, actor); err != nil {
		return err
	}
	if err = insertAccountHistory(ctx, tx, a, actor, reason); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *IdentityAdministrationRepository) ResetCredentials(ctx context.Context, id, passwordHash, totp string, expected int64, actor, reason string, now time.Time) (identity.Account, error) {
	secret, err := r.Secrets.Seal("identity:totp:"+id, totp)
	if err != nil {
		return identity.Account{}, err
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return identity.Account{}, err
	}
	defer tx.Rollback()
	a, err := getAccountTx(ctx, tx, id)
	if err != nil {
		return identity.Account{}, err
	}
	if a.Version != expected {
		return identity.Account{}, identity.ErrAccountConflict
	}
	_, err = tx.ExecContext(ctx, `UPDATE internal_user_credentials SET password_hash=$2,totp_secret_ciphertext=$3,credential_version=credential_version+1,failed_login_count=0,locked_until=NULL,password_changed_at=$4,updated_at=$4 WHERE user_id=$1::uuid`, id, passwordHash, secret, now)
	if err != nil {
		return identity.Account{}, err
	}
	a.Version++
	a.MFARequired = true
	a.UpdatedAt = now.UTC()
	res, err := tx.ExecContext(ctx, `UPDATE internal_users SET mfa_required=true,version=$3,updated_at=$4,updated_by=$5::uuid WHERE id=$1::uuid AND version=$2`, id, expected, a.Version, a.UpdatedAt, actor)
	if err != nil {
		return identity.Account{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return identity.Account{}, err
	}
	if n == 0 {
		return identity.Account{}, identity.ErrAccountConflict
	}
	if err = insertAccountHistory(ctx, tx, a, actor, reason); err != nil {
		return identity.Account{}, err
	}
	if err = tx.Commit(); err != nil {
		return identity.Account{}, err
	}
	return a, nil
}
func (r *IdentityAdministrationRepository) UnlockAccount(ctx context.Context, id string, expected int64, actor, reason string, now time.Time) (identity.Account, error) {
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return identity.Account{}, err
	}
	defer tx.Rollback()
	a, err := getAccountTx(ctx, tx, id)
	if err != nil {
		return identity.Account{}, err
	}
	if a.Version != expected {
		return identity.Account{}, identity.ErrAccountConflict
	}
	_, err = tx.ExecContext(ctx, `UPDATE internal_user_credentials SET failed_login_count=0,locked_until=NULL,updated_at=$2 WHERE user_id=$1::uuid`, id, now)
	if err != nil {
		return identity.Account{}, err
	}
	a.Version++
	a.UpdatedAt = now.UTC()
	res, err := tx.ExecContext(ctx, `UPDATE internal_users SET version=$3,updated_at=$4,updated_by=$5::uuid WHERE id=$1::uuid AND version=$2`, id, expected, a.Version, a.UpdatedAt, actor)
	if err != nil {
		return identity.Account{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return identity.Account{}, err
	}
	if n == 0 {
		return identity.Account{}, identity.ErrAccountConflict
	}
	if err = insertAccountHistory(ctx, tx, a, actor, reason); err != nil {
		return identity.Account{}, err
	}
	if err = tx.Commit(); err != nil {
		return identity.Account{}, err
	}
	return a, nil
}

func getAccountTx(ctx context.Context, tx *sql.Tx, id string) (identity.Account, error) {
	var lockedID string
	if err := tx.QueryRowContext(ctx, `SELECT id::text FROM internal_users WHERE id=$1::uuid FOR UPDATE`, id).Scan(&lockedID); errors.Is(err, sql.ErrNoRows) {
		return identity.Account{}, identity.ErrAccountNotFound
	} else if err != nil {
		return identity.Account{}, err
	}
	a, err := scanAccount(tx.QueryRowContext(ctx, accountSelect+` WHERE u.id=$1::uuid GROUP BY u.id`, lockedID))
	if errors.Is(err, sql.ErrNoRows) {
		return identity.Account{}, identity.ErrAccountNotFound
	}
	return a, err
}
func validateRoleCodesTx(ctx context.Context, tx *sql.Tx, codes []string) error {
	payload, err := json.Marshal(codes)
	if err != nil {
		return err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM roles WHERE code IN (SELECT jsonb_array_elements_text($1::jsonb))`, string(payload)).Scan(&count); err != nil {
		return err
	}
	if count != len(codes) {
		return identity.ErrUnknownRole
	}
	return nil
}
func replaceRolesTx(ctx context.Context, tx *sql.Tx, userID string, codes []string, actor string) error {
	payload, err := json.Marshal(codes)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM user_roles WHERE user_id=$1::uuid`, userID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO user_roles(user_id,role_id,assigned_by) SELECT $1::uuid,id,$3::uuid FROM roles WHERE code IN (SELECT jsonb_array_elements_text($2::jsonb))`, userID, string(payload), actor)
	return err
}
func insertAccountHistory(ctx context.Context, tx *sql.Tx, a identity.Account, actor, reason string) error {
	snapshot, err := json.Marshal(a)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO internal_user_change_history(user_id,user_version,actor_id,reason,snapshot) VALUES($1::uuid,$2,$3::uuid,$4,$5::jsonb)`, a.ID, a.Version, actor, reason, string(snapshot))
	return err
}
func hasRoleCode(codes []string, target string) bool {
	for _, v := range codes {
		if v == target {
			return true
		}
	}
	return false
}

// pqTextArray renders controlled role codes as a PostgreSQL array literal. Codes are
// server-normalised to uppercase identifiers and cannot contain quotes or commas.
func pqTextArray(values []string) string { return "{" + strings.Join(values, ",") + "}" }

var _ = fmt.Sprintf
