// Package identity binds a Keycloak subject to a Confluence user.
//
// The durable identity is (issuer, sub) ↔ (instance, userKey). A username is
// only a way to find a candidate: a first-contact match by preferred_username
// creates a mapping only when the administrator has declared that Keycloak and
// Confluence read the same directory. Otherwise ownership is proven by an
// administrator's confirmation or by the user signing in to Confluence with
// their own credential. Names and e-mail addresses are never matched loosely,
// and a mapping never moves to a different userKey on its own: an account
// deleted and recreated under the same name gets a new key and stays unmapped.
package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hkjang/confmcp/internal/confluence"
	"github.com/hkjang/confmcp/internal/permission"
	"github.com/hkjang/confmcp/internal/settings"
)

// ErrUnmapped means no Confluence account is bound to the subject.
var ErrUnmapped = errors.New("IDENTITY_UNMAPPED")

// Evidence values: how ownership of the Confluence account was established.
const (
	EvidenceSameDirectory = "same_directory"
	EvidenceAdmin         = "admin_confirmed"
	EvidenceDelegated     = "delegated_auth"
)

// Mapping is a stored identity binding.
type Mapping struct {
	ID                 int64      `json:"id"`
	KeycloakIssuer     string     `json:"keycloakIssuer"`
	KeycloakSub        string     `json:"keycloakSub"`
	KeycloakUsername   string     `json:"keycloakUsername"`
	InstanceID         string     `json:"instanceId"`
	ConfluenceUserKey  string     `json:"confluenceUserKey"`
	ConfluenceUsername string     `json:"confluenceUsername"`
	ConfluenceDisplay  string     `json:"confluenceDisplay"`
	ConfluenceEmail    string     `json:"confluenceEmail"`
	MappingType        string     `json:"mappingType"`
	Evidence           string     `json:"evidence"`
	Active             bool       `json:"active"`
	LastError          string     `json:"lastError,omitempty"`
	MappedAt           time.Time  `json:"mappedAt"`
	VerifiedAt         *time.Time `json:"verifiedAt,omitempty"`
}

// MappingError is a failed mapping attempt surfaced in the admin UI.
type MappingError struct {
	ID               int64     `json:"id"`
	KeycloakSub      string    `json:"keycloakSub"`
	KeycloakUsername string    `json:"keycloakUsername"`
	Reason           string    `json:"reason"`
	Occurrences      int       `json:"occurrences"`
	FirstSeenAt      time.Time `json:"firstSeenAt"`
	OccurredAt       time.Time `json:"occurredAt"`
}

// HistoryEntry is one change to a subject's mapping.
type HistoryEntry struct {
	ID                 int64     `json:"id"`
	KeycloakSub        string    `json:"keycloakSub"`
	Action             string    `json:"action"`
	ConfluenceUserKey  string    `json:"confluenceUserKey,omitempty"`
	ConfluenceUsername string    `json:"confluenceUsername,omitempty"`
	Actor              string    `json:"actor,omitempty"`
	Note               string    `json:"note,omitempty"`
	OccurredAt         time.Time `json:"occurredAt"`
}

// Mapper resolves and stores identity bindings.
type Mapper struct {
	pool     *pgxpool.Pool
	store    *settings.Store
	provider *confluence.Provider
	resolver *permission.Resolver
}

// NewMapper builds the mapper.
func NewMapper(pool *pgxpool.Pool, store *settings.Store, provider *confluence.Provider, resolver *permission.Resolver) *Mapper {
	return &Mapper{pool: pool, store: store, provider: provider, resolver: resolver}
}

const mapCols = `id, keycloak_issuer, keycloak_sub, keycloak_username, instance_id, confluence_user_key,
	confluence_username, COALESCE(confluence_display,''), COALESCE(confluence_email,''),
	mapping_type, evidence, active, COALESCE(last_error,''), mapped_at, verified_at`

func scanMapping(row pgx.Row) (*Mapping, error) {
	var m Mapping
	err := row.Scan(&m.ID, &m.KeycloakIssuer, &m.KeycloakSub, &m.KeycloakUsername, &m.InstanceID,
		&m.ConfluenceUserKey, &m.ConfluenceUsername, &m.ConfluenceDisplay, &m.ConfluenceEmail,
		&m.MappingType, &m.Evidence, &m.Active, &m.LastError, &m.MappedAt, &m.VerifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUnmapped
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// BySub loads a mapping by Keycloak subject.
func (m *Mapper) BySub(ctx context.Context, sub string) (*Mapping, error) {
	return scanMapping(m.pool.QueryRow(ctx,
		`SELECT `+mapCols+` FROM confluence_identity_mapping WHERE keycloak_sub=$1`, sub))
}

// List returns mappings for the admin UI.
func (m *Mapper) List(ctx context.Context, q, state string, limit int) ([]Mapping, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := m.pool.Query(ctx, `SELECT `+mapCols+` FROM confluence_identity_mapping
		WHERE ($1='' OR keycloak_username ILIKE '%'||$1||'%' OR confluence_username ILIKE '%'||$1||'%')
		  AND ($2='' OR ($2='active' AND active) OR ($2='inactive' AND NOT active)
		       OR ($2='unverified' AND verified_at IS NULL) OR ($2='error' AND last_error IS NOT NULL))
		ORDER BY keycloak_username LIMIT $3`, q, state, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Mapping{}
	for rows.Next() {
		one, err := scanMapping(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *one)
	}
	return out, rows.Err()
}

// Resolve returns the Confluence identity of a subject, discovering it on
// first contact when the directory is trusted.
func (m *Mapper) Resolve(ctx context.Context, issuer, sub, username string) (*Mapping, error) {
	if existing, err := m.BySub(ctx, sub); err == nil {
		if !existing.Active {
			return nil, fmt.Errorf("%w: 매핑이 비활성 상태입니다 (%s)", ErrUnmapped, existing.ConfluenceUsername)
		}
		if existing.KeycloakUsername != username && username != "" {
			_, _ = m.pool.Exec(ctx,
				`UPDATE confluence_identity_mapping SET keycloak_username=$2, updated_at=NOW() WHERE keycloak_sub=$1`,
				sub, username)
			existing.KeycloakUsername = username
		}
		return existing, nil
	} else if !errors.Is(err, ErrUnmapped) {
		return nil, err
	}
	return m.autoMap(ctx, issuer, sub, username)
}

// lookup finds a Confluence account by username or key. The plugin is asked
// first because it also reports deactivation and duplicate directories.
func (m *Mapper) lookup(ctx context.Context, username, key string) (*confluence.User, error) {
	if m.resolver != nil && m.resolver.Mode(ctx) == "plugin" {
		pu, err := m.resolver.Plugin().User(ctx, username, key)
		if err == nil {
			if pu.Duplicates > 1 {
				return nil, fmt.Errorf("같은 사용자명이 여러 디렉터리에 있습니다 (%d건)", pu.Duplicates)
			}
			return &confluence.User{Username: pu.Username, UserKey: pu.UserKey, DisplayName: pu.DisplayName,
				Email: pu.Email, Status: pu.Status}, nil
		}
		perm, _ := m.store.Permission(ctx)
		if perm.PluginSecretEnc != "" {
			return nil, err
		}
	}
	adapter, _, err := m.provider.Adapter(ctx)
	if err != nil {
		return nil, err
	}
	cred, err := m.provider.ServiceCredential(ctx)
	if err != nil {
		return nil, err
	}
	if key != "" {
		return adapter.UserByKey(ctx, cred, key)
	}
	return adapter.UserByUsername(ctx, cred, username)
}

func (m *Mapper) instanceID(ctx context.Context) string {
	cfg, _ := m.store.Confluence(ctx)
	if strings.TrimSpace(cfg.InstanceID) == "" {
		return "default"
	}
	return cfg.InstanceID
}

// autoMap performs first-contact discovery, which only creates a mapping when
// the administrator vouched for a shared directory.
func (m *Mapper) autoMap(ctx context.Context, issuer, sub, username string) (*Mapping, error) {
	cfg, err := m.store.Confluence(ctx)
	if err != nil {
		return nil, err
	}
	if cfg.BaseURL == "" {
		// Not configured yet: a deployment state, not a per-user failure.
		return nil, fmt.Errorf("%w: Confluence 연결이 설정되지 않았습니다", ErrUnmapped)
	}
	if !cfg.TrustSameDirectory {
		reason := "자동 매핑이 꺼져 있습니다. 관리자 확인 또는 내 Confluence 연결(사용자 위임 인증)로 계정을 확인해야 합니다"
		m.recordError(ctx, sub, username, reason)
		return nil, fmt.Errorf("%w: %s", ErrUnmapped, reason)
	}
	u, err := m.lookup(ctx, username, "")
	if err != nil {
		m.recordError(ctx, sub, username, "Confluence 사용자 조회 실패: "+err.Error())
		return nil, fmt.Errorf("%w: %v", ErrUnmapped, err)
	}
	if !strings.EqualFold(u.Username, username) {
		reason := fmt.Sprintf("사용자명이 정확히 일치하지 않습니다 (%s ≠ %s)", u.Username, username)
		m.recordError(ctx, sub, username, reason)
		return nil, fmt.Errorf("%w: %s", ErrUnmapped, reason)
	}
	if u.Status != "" && !strings.EqualFold(u.Status, "active") {
		reason := fmt.Sprintf("Confluence 계정 %s 이 비활성 상태입니다", u.Username)
		m.recordError(ctx, sub, username, reason)
		return nil, fmt.Errorf("%w: %s", ErrUnmapped, reason)
	}
	return m.upsert(ctx, issuer, sub, username, u, "auto", EvidenceSameDirectory, "system")
}

// upsert writes a mapping, refusing a Confluence account already bound to a
// different subject and refusing to move an existing mapping to another key.
func (m *Mapper) upsert(ctx context.Context, issuer, sub, username string, u *confluence.User, kind, evidence, actor string) (*Mapping, error) {
	instance := m.instanceID(ctx)
	var otherSub string
	err := m.pool.QueryRow(ctx,
		`SELECT keycloak_sub FROM confluence_identity_mapping WHERE instance_id=$1 AND confluence_user_key=$2`,
		instance, u.UserKey).Scan(&otherSub)
	if err == nil && otherSub != sub {
		reason := fmt.Sprintf("Confluence 사용자 %s 는 이미 다른 Keycloak 주체에 매핑되어 있습니다", u.Username)
		m.recordError(ctx, sub, username, reason)
		return nil, fmt.Errorf("%w: %s", ErrUnmapped, reason)
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if existing, err := m.BySub(ctx, sub); err == nil && existing.ConfluenceUserKey != u.UserKey && kind == "auto" {
		return nil, fmt.Errorf("%w: 기존 매핑(%s)과 다른 계정입니다. 관리자가 매핑을 변경해야 합니다",
			ErrUnmapped, existing.ConfluenceUsername)
	}
	_, err = m.pool.Exec(ctx, `
		INSERT INTO confluence_identity_mapping(keycloak_issuer, keycloak_sub, keycloak_username, instance_id,
			confluence_user_key, confluence_username, confluence_display, confluence_email,
			mapping_type, evidence, active, verified_at, last_error)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,TRUE,NOW(),NULL)
		ON CONFLICT (keycloak_sub) DO UPDATE
		   SET keycloak_issuer = EXCLUDED.keycloak_issuer,
		       keycloak_username = EXCLUDED.keycloak_username,
		       instance_id = EXCLUDED.instance_id,
		       confluence_user_key = EXCLUDED.confluence_user_key,
		       confluence_username = EXCLUDED.confluence_username,
		       confluence_display = EXCLUDED.confluence_display,
		       confluence_email = EXCLUDED.confluence_email,
		       mapping_type = EXCLUDED.mapping_type,
		       evidence = EXCLUDED.evidence,
		       active = TRUE,
		       verified_at = NOW(),
		       last_error = NULL,
		       updated_at = NOW()`,
		issuer, sub, username, instance, u.UserKey, u.Username, u.DisplayName, u.Email, kind, evidence)
	if err != nil {
		return nil, err
	}
	_, _ = m.pool.Exec(ctx, `DELETE FROM identity_mapping_errors WHERE keycloak_sub=$1`, sub)
	m.history(ctx, sub, "map:"+evidence, u.UserKey, u.Username, actor, "")
	return m.BySub(ctx, sub)
}

// Verify re-checks a stored mapping: the user key must still exist and be
// active. A username that now points at a different key (the account was
// recreated) does not move the mapping; it is deactivated instead.
func (m *Mapper) Verify(ctx context.Context, sub, actor string) (*Mapping, error) {
	existing, err := m.BySub(ctx, sub)
	if err != nil {
		return nil, err
	}
	fail := func(reason string, deactivate bool) (*Mapping, error) {
		_, _ = m.pool.Exec(ctx, `UPDATE confluence_identity_mapping
			SET last_error=$2, verified_at=NULL, active = CASE WHEN $3 THEN FALSE ELSE active END, updated_at=NOW()
			WHERE keycloak_sub=$1`, sub, reason, deactivate)
		m.history(ctx, sub, "verify_failed", existing.ConfluenceUserKey, existing.ConfluenceUsername, actor, reason)
		return nil, errors.New(reason)
	}
	byKey, err := m.lookup(ctx, "", existing.ConfluenceUserKey)
	if err != nil {
		if confluence.IsStatus(err, 404) {
			return fail("Confluence 에서 userKey 를 찾을 수 없습니다 (계정 삭제 가능성)", true)
		}
		return fail("Confluence 사용자 조회 실패: "+err.Error(), false)
	}
	if byKey.Status != "" && !strings.EqualFold(byKey.Status, "active") {
		return fail("Confluence 계정이 비활성 상태입니다", true)
	}
	if byName, err := m.lookup(ctx, existing.ConfluenceUsername, ""); err == nil && byName.UserKey != existing.ConfluenceUserKey {
		return fail(fmt.Sprintf("사용자명 %s 이 다른 계정(userKey %s)을 가리킵니다. 재생성된 계정은 자동으로 승계하지 않습니다",
			existing.ConfluenceUsername, byName.UserKey), true)
	}
	_, err = m.pool.Exec(ctx, `UPDATE confluence_identity_mapping
		SET confluence_username=$2, confluence_display=$3, confluence_email=$4, verified_at=NOW(), last_error=NULL, updated_at=NOW()
		WHERE keycloak_sub=$1`, sub, byKey.Username, byKey.DisplayName, byKey.Email)
	if err != nil {
		return nil, err
	}
	m.history(ctx, sub, "verified", byKey.UserKey, byKey.Username, actor, "")
	return m.BySub(ctx, sub)
}

// BindDelegated records a mapping proven by the user's own Confluence
// sign-in. An existing mapping to a different key is never overwritten.
func (m *Mapper) BindDelegated(ctx context.Context, issuer, sub, username string, u *confluence.User) (*Mapping, error) {
	if existing, err := m.BySub(ctx, sub); err == nil {
		if existing.ConfluenceUserKey != u.UserKey {
			return nil, fmt.Errorf("연결한 Confluence 계정(%s)이 매핑된 계정(%s)과 다릅니다", u.Username, existing.ConfluenceUsername)
		}
		_, _ = m.pool.Exec(ctx, `UPDATE confluence_identity_mapping SET verified_at=NOW(), last_error=NULL, updated_at=NOW() WHERE keycloak_sub=$1`, sub)
		m.history(ctx, sub, "verified:delegated", u.UserKey, u.Username, username, "")
		return m.BySub(ctx, sub)
	}
	return m.upsert(ctx, issuer, sub, username, u, "delegated", EvidenceDelegated, username)
}

// SetActive enables or disables a mapping.
func (m *Mapper) SetActive(ctx context.Context, sub string, active bool, actor string) error {
	_, err := m.pool.Exec(ctx,
		`UPDATE confluence_identity_mapping SET active=$2, updated_at=NOW() WHERE keycloak_sub=$1`, sub, active)
	action := "deactivate"
	if active {
		action = "activate"
	}
	m.history(ctx, sub, action, "", "", actor, "")
	return err
}

// Delete removes a mapping so it can be rebuilt.
func (m *Mapper) Delete(ctx context.Context, sub, actor string) error {
	existing, _ := m.BySub(ctx, sub)
	_, err := m.pool.Exec(ctx, `DELETE FROM confluence_identity_mapping WHERE keycloak_sub=$1`, sub)
	if existing != nil {
		m.history(ctx, sub, "delete", existing.ConfluenceUserKey, existing.ConfluenceUsername, actor, "")
	}
	return err
}

// ManualMap binds a subject to a Confluence account chosen and confirmed by an
// administrator.
func (m *Mapper) ManualMap(ctx context.Context, issuer, sub, keycloakUsername, confluenceUsername, actor string) (*Mapping, error) {
	u, err := m.lookup(ctx, confluenceUsername, "")
	if err != nil {
		return nil, err
	}
	if u.Status != "" && !strings.EqualFold(u.Status, "active") {
		return nil, fmt.Errorf("Confluence 계정 %s 이 비활성 상태입니다", u.Username)
	}
	return m.upsert(ctx, issuer, sub, keycloakUsername, u, "manual", EvidenceAdmin, actor)
}

func (m *Mapper) history(ctx context.Context, sub, action, key, username, actor, note string) {
	_, _ = m.pool.Exec(ctx, `INSERT INTO identity_mapping_history(keycloak_sub, action, confluence_user_key,
		confluence_username, actor, note) VALUES ($1,$2,NULLIF($3,''),NULLIF($4,''),NULLIF($5,''),NULLIF($6,''))`,
		sub, action, key, username, actor, note)
}

// History lists mapping changes, newest first.
func (m *Mapper) History(ctx context.Context, sub string, limit int) ([]HistoryEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := m.pool.Query(ctx, `SELECT id, keycloak_sub, action, COALESCE(confluence_user_key,''),
		COALESCE(confluence_username,''), COALESCE(actor,''), COALESCE(note,''), occurred_at
		FROM identity_mapping_history WHERE ($1='' OR keycloak_sub=$1) ORDER BY occurred_at DESC LIMIT $2`, sub, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HistoryEntry{}
	for rows.Next() {
		var h HistoryEntry
		if err := rows.Scan(&h.ID, &h.KeycloakSub, &h.Action, &h.ConfluenceUserKey, &h.ConfluenceUsername,
			&h.Actor, &h.Note, &h.OccurredAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// recordError stores one row per subject, counting repeats instead of growing
// the table on every request that retries a failed lookup.
func (m *Mapper) recordError(ctx context.Context, sub, username, reason string) {
	_, _ = m.pool.Exec(ctx, `
		INSERT INTO identity_mapping_errors(keycloak_sub, keycloak_username, reason)
		VALUES ($1,$2,$3)
		ON CONFLICT (keycloak_sub) DO UPDATE
		   SET keycloak_username = EXCLUDED.keycloak_username,
		       reason = EXCLUDED.reason,
		       occurrences = identity_mapping_errors.occurrences + 1,
		       occurred_at = NOW()`, sub, username, reason)
}

// Errors lists recent mapping failures.
func (m *Mapper) Errors(ctx context.Context, limit int) ([]MappingError, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := m.pool.Query(ctx,
		`SELECT id, keycloak_sub, keycloak_username, reason, occurrences, first_seen_at, occurred_at
		 FROM identity_mapping_errors ORDER BY occurred_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MappingError{}
	for rows.Next() {
		var e MappingError
		if err := rows.Scan(&e.ID, &e.KeycloakSub, &e.KeycloakUsername, &e.Reason,
			&e.Occurrences, &e.FirstSeenAt, &e.OccurredAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ClearErrors empties the mapping error list.
func (m *Mapper) ClearErrors(ctx context.Context) error {
	_, err := m.pool.Exec(ctx, `DELETE FROM identity_mapping_errors`)
	return err
}
