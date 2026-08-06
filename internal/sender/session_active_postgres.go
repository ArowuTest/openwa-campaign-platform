package sender

import (
	"context"
	"database/sql"
	"errors"
)

type PostgreSQLActiveSessionWorkChecker struct{ DB *sql.DB }

func (c PostgreSQLActiveSessionWorkChecker) HasActiveSessionWork(ctx context.Context, sessionID string) (bool, error) {
	if c.DB == nil {
		return false, errors.New("database is required")
	}
	var active bool
	err := c.DB.QueryRowContext(ctx, `SELECT EXISTS(
SELECT 1 FROM campaign_recipients WHERE assigned_session_id=$1::uuid AND status IN ('AUTHORISED','QUEUED','CLAIMED','SUBMITTING','GATEWAY_ACCEPTED','FAILED_RETRYABLE','UNKNOWN')
UNION ALL SELECT 1 FROM test_message_sends WHERE sender_session_id=$1::uuid AND status IN ('PENDING','PROCESSING','UNKNOWN')
)`, sessionID).Scan(&active)
	return active, err
}
