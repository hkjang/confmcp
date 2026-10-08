// Package operation records write executions so that an approved change is
// sent to Confluence at most once.
//
// There is no distributed transaction between Confluence and this database,
// so exactly-once is not claimed. What is guaranteed: the same idempotency
// key never starts a second upstream write; a write whose outcome is unclear
// is recorded as outcome_unknown and is not retried automatically.
package operation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hkjang/confmcp/internal/crypto"
)

// Statuses of an operation.
const (
	StatusPending        = "pending"
	StatusExecuting      = "executing"
	StatusSucceeded      = "succeeded"
	StatusFailed         = "failed"
	StatusOutcomeUnknown = "outcome_unknown"
)

// Record is one write execution.
type Record struct {
	ID             uuid.UUID  `json:"id"`
	IdempotencyKey string     `json:"idempotencyKey"`
	KeycloakSub    string     `json:"keycloakSub"`
	Username       string     `json:"username"`
	ToolName       string     `json:"toolName"`
	ArgumentsHash  string     `json:"argumentsHash"`
	ApprovalID     *uuid.UUID `json:"approvalId,omitempty"`
	TargetID       string     `json:"targetId,omitempty"`
	Status         string     `json:"status"`
	UpstreamID     string     `json:"upstreamId,omitempty"`
	ResultVersion  *int       `json:"resultVersion,omitempty"`
	ErrorCode      string     `json:"errorCode,omitempty"`
	Message        string     `json:"message,omitempty"`
	ExecutedBy     string     `json:"executedBy,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	ResolvedBy     string     `json:"resolvedBy,omitempty"`
	ResolvedAt     *time.Time `json:"resolvedAt,omitempty"`
}

// Store persists operation records.
type Store struct{ pool *pgxpool.Pool }

// New builds the store.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Key derives the idempotency key of a write.
func Key(sub, tool, argsHash string, approvalID *uuid.UUID) string {
	a := ""
	if approvalID != nil {
		a = approvalID.String()
	}
	return crypto.SHA256Hex(sub + "\n" + tool + "\n" + argsHash + "\n" + a)
}

// ErrInFlight means the same write already started.
var ErrInFlight = errors.New("동일한 쓰기 요청이 이미 실행 중이거나 결과가 불명확합니다")

const cols = `id, idempotency_key, keycloak_sub, username, tool_name, arguments_hash, approval_id, target_id,
	status, COALESCE(upstream_id,''), result_version, COALESCE(error_code,''), COALESCE(message,''),
	COALESCE(executed_by,''), created_at, updated_at, COALESCE(resolved_by,''), resolved_at`

func scan(row pgx.Row) (*Record, error) {
	var r Record
	err := row.Scan(&r.ID, &r.IdempotencyKey, &r.KeycloakSub, &r.Username, &r.ToolName, &r.ArgumentsHash,
		&r.ApprovalID, &r.TargetID, &r.Status, &r.UpstreamID, &r.ResultVersion, &r.ErrorCode, &r.Message,
		&r.ExecutedBy, &r.CreatedAt, &r.UpdatedAt, &r.ResolvedBy, &r.ResolvedAt)
	return &r, err
}

// Begin reserves the idempotency key. When a record with the same key exists
// it is returned with ErrInFlight (still running or unclear) or as is (done),
// and the caller must not write again.
func (s *Store) Begin(ctx context.Context, r Record) (*Record, error) {
	r.ID = uuid.New()
	_, err := s.pool.Exec(ctx, `INSERT INTO operation_records(id, idempotency_key, keycloak_sub, username,
		tool_name, arguments_hash, approval_id, target_id, status, executed_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		r.ID, r.IdempotencyKey, r.KeycloakSub, r.Username, r.ToolName, r.ArgumentsHash, r.ApprovalID,
		r.TargetID, StatusExecuting, r.ExecutedBy)
	if err == nil {
		return s.ByID(ctx, r.ID)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return nil, err
	}
	existing, lerr := scan(s.pool.QueryRow(ctx, `SELECT `+cols+` FROM operation_records WHERE idempotency_key=$1`, r.IdempotencyKey))
	if lerr != nil {
		return nil, lerr
	}
	switch existing.Status {
	case StatusSucceeded, StatusFailed:
		return existing, nil
	default:
		return existing, fmt.Errorf("%w (%s)", ErrInFlight, existing.Status)
	}
}

// Finish records the outcome.
func (s *Store) Finish(ctx context.Context, id uuid.UUID, status, upstreamID string, version *int, code, message string) {
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, _ = s.pool.Exec(wctx, `UPDATE operation_records SET status=$2, upstream_id=NULLIF($3,''), result_version=$4,
		error_code=NULLIF($5,''), message=NULLIF($6,''), updated_at=NOW() WHERE id=$1`,
		id, status, upstreamID, version, code, message)
}

// ByID loads one record.
func (s *Store) ByID(ctx context.Context, id uuid.UUID) (*Record, error) {
	return scan(s.pool.QueryRow(ctx, `SELECT `+cols+` FROM operation_records WHERE id=$1`, id))
}

// List returns records, newest first.
func (s *Store) List(ctx context.Context, status, sub string, limit int) ([]Record, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `SELECT `+cols+` FROM operation_records
		WHERE ($1='' OR status=$1) AND ($2='' OR keycloak_sub=$2) ORDER BY created_at DESC LIMIT $3`, status, sub, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// Resolve lets an administrator settle an outcome_unknown record after
// checking Confluence by hand.
func (s *Store) Resolve(ctx context.Context, id uuid.UUID, status, actor, note string) error {
	if status != StatusSucceeded && status != StatusFailed {
		return errors.New("succeeded 또는 failed 로만 정리할 수 있습니다")
	}
	tag, err := s.pool.Exec(ctx, `UPDATE operation_records SET status=$2, resolved_by=$3, resolved_at=NOW(),
		message = COALESCE(message,'') || CASE WHEN $4='' THEN '' ELSE ' / 정리: ' || $4 END, updated_at=NOW()
		WHERE id=$1 AND status IN ('outcome_unknown','executing')`, id, status, actor, note)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("정리할 수 있는 상태가 아닙니다")
	}
	return nil
}

// MarkAbandoned turns executions that never finished (process restart in the
// middle of a write) into outcome_unknown.
func (s *Store) MarkAbandoned(ctx context.Context, olderThan time.Duration) error {
	_, err := s.pool.Exec(ctx, `UPDATE operation_records SET status='outcome_unknown',
		message='실행 중 서비스가 중단되어 결과를 확인하지 못했습니다', updated_at=NOW()
		WHERE status='executing' AND updated_at < NOW() - make_interval(secs => $1)`, olderThan.Seconds())
	return err
}

// ByKey loads the record of an idempotency key, if any.
func (s *Store) ByKey(ctx context.Context, key string) (*Record, error) {
	return scan(s.pool.QueryRow(ctx, `SELECT `+cols+` FROM operation_records WHERE idempotency_key=$1`, key))
}
