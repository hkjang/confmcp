package confluence

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hkjang/confmcp/internal/settings"
)

// Provider builds adapters from the admin-managed settings and resolves the
// credential a call should run under.
type Provider struct {
	store *settings.Store
	pool  *pgxpool.Pool

	mu     sync.Mutex
	key    string
	client *Client
}

// NewProvider builds the provider.
func NewProvider(store *settings.Store, pool *pgxpool.Pool) *Provider {
	return &Provider{store: store, pool: pool}
}

// Reset drops the cached client after a settings change.
func (p *Provider) Reset() {
	p.mu.Lock()
	p.client, p.key = nil, ""
	p.mu.Unlock()
}

// Adapter returns an adapter for the current settings.
func (p *Provider) Adapter(ctx context.Context) (Adapter, settings.Confluence, error) {
	cfg, err := p.store.Confluence(ctx)
	if err != nil {
		return nil, cfg, err
	}
	key := fmt.Sprintf("%s|%d|%t", cfg.BaseURL, cfg.TimeoutSec, cfg.InsecureSkipTLS)

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.key == key && p.client != nil {
		return p.client, cfg, nil
	}
	c, err := NewClient(cfg)
	if err != nil {
		return nil, cfg, err
	}
	p.client, p.key = c, key
	return c, cfg, nil
}

// ServiceCredential returns the service account credential.
func (p *Provider) ServiceCredential(ctx context.Context) (Credential, error) {
	cfg, err := p.store.Confluence(ctx)
	if err != nil {
		return Credential{}, err
	}
	pw, err := p.store.Reveal(cfg.ServicePasswordEnc)
	if err != nil {
		return Credential{}, err
	}
	if strings.TrimSpace(cfg.ServiceUsername) == "" || pw == "" {
		return Credential{}, ErrNoServiceCredential
	}
	return Credential{Mode: "service", Username: cfg.ServiceUsername, Password: pw}, nil
}

// UserCredentialInfo describes a user's delegated credential without the secret.
type UserCredentialInfo struct {
	UserID             int64      `json:"userId"`
	ConfluenceUsername string     `json:"confluenceUsername"`
	ConfluenceUserKey  string     `json:"confluenceUserKey,omitempty"`
	HasSecret          bool       `json:"hasSecret"`
	VerifiedAt         *time.Time `json:"verifiedAt,omitempty"`
	UpdatedAt          *time.Time `json:"updatedAt,omitempty"`
}

// UserCredential returns the user's own delegated credential. It is only
// usable after a successful verification proved it belongs to the user's
// mapped Confluence account.
func (p *Provider) UserCredential(ctx context.Context, userID int64) (Credential, string, error) {
	var username, key, enc string
	var verified *time.Time
	err := p.pool.QueryRow(ctx, `SELECT confluence_username, COALESCE(confluence_user_key,''), secret_enc, verified_at
		FROM user_confluence_credential WHERE user_id=$1`, userID).Scan(&username, &key, &enc, &verified)
	if errors.Is(err, pgx.ErrNoRows) {
		return Credential{}, "", ErrNoUserCredential
	}
	if err != nil {
		return Credential{}, "", err
	}
	if verified == nil {
		return Credential{}, "", errors.New("사용자 자격증명이 아직 검증되지 않았습니다")
	}
	pw, err := p.store.Reveal(enc)
	if err != nil {
		return Credential{}, "", err
	}
	if pw == "" {
		return Credential{}, "", ErrNoUserCredential
	}
	return Credential{Mode: "delegated", Username: username, Password: pw}, key, nil
}

// SaveUserCredential stores a user's credential sealed at rest, unverified.
func (p *Provider) SaveUserCredential(ctx context.Context, userID int64, username, secret string) error {
	enc, err := p.store.Seal(secret)
	if err != nil {
		return err
	}
	_, err = p.pool.Exec(ctx, `
		INSERT INTO user_confluence_credential(user_id, confluence_username, secret_enc, updated_at)
		VALUES ($1,$2,$3,NOW())
		ON CONFLICT (user_id) DO UPDATE
		   SET confluence_username = EXCLUDED.confluence_username,
		       secret_enc = EXCLUDED.secret_enc,
		       confluence_user_key = NULL,
		       verified_at = NULL,
		       updated_at = NOW()`, userID, username, enc)
	return err
}

// DeleteUserCredential revokes a user's credential.
func (p *Provider) DeleteUserCredential(ctx context.Context, userID int64) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM user_confluence_credential WHERE user_id=$1`, userID)
	return err
}

// VerifyUserCredential checks the stored credential against Confluence and
// records the user key it authenticated as. The caller compares that key with
// the user's identity mapping.
func (p *Provider) VerifyUserCredential(ctx context.Context, userID int64) (*User, error) {
	var username, enc string
	err := p.pool.QueryRow(ctx, `SELECT confluence_username, secret_enc FROM user_confluence_credential WHERE user_id=$1`,
		userID).Scan(&username, &enc)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoUserCredential
	}
	if err != nil {
		return nil, err
	}
	pw, err := p.store.Reveal(enc)
	if err != nil {
		return nil, err
	}
	adapter, _, err := p.Adapter(ctx)
	if err != nil {
		return nil, err
	}
	u, err := adapter.CurrentUser(ctx, Credential{Mode: "delegated", Username: username, Password: pw})
	if err != nil {
		_, _ = p.pool.Exec(ctx, `UPDATE user_confluence_credential SET verified_at=NULL WHERE user_id=$1`, userID)
		return nil, err
	}
	_, err = p.pool.Exec(ctx, `UPDATE user_confluence_credential
		SET verified_at=NOW(), confluence_user_key=$2 WHERE user_id=$1`, userID, u.UserKey)
	return u, err
}

// UserCredentialInfo reports whether a user has a credential registered.
func (p *Provider) UserCredentialInfo(ctx context.Context, userID int64) (*UserCredentialInfo, error) {
	out := &UserCredentialInfo{UserID: userID}
	err := p.pool.QueryRow(ctx, `
		SELECT confluence_username, COALESCE(confluence_user_key,''), (secret_enc <> ''), verified_at, updated_at
		FROM user_confluence_credential WHERE user_id=$1`, userID).
		Scan(&out.ConfluenceUsername, &out.ConfluenceUserKey, &out.HasSecret, &out.VerifiedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}
