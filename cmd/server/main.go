// Command server runs the confmcp gateway: a Confluence Server MCP gateway
// with Keycloak SSO, an OAuth authorization server for MCP clients, an admin
// console and personal key management.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hkjang/confmcp/internal/aiproxy"
	"github.com/hkjang/confmcp/internal/api"
	"github.com/hkjang/confmcp/internal/apikey"
	"github.com/hkjang/confmcp/internal/approval"
	"github.com/hkjang/confmcp/internal/attachment"
	"github.com/hkjang/confmcp/internal/audit"
	"github.com/hkjang/confmcp/internal/auth"
	"github.com/hkjang/confmcp/internal/config"
	"github.com/hkjang/confmcp/internal/confluence"
	"github.com/hkjang/confmcp/internal/crypto"
	"github.com/hkjang/confmcp/internal/database"
	"github.com/hkjang/confmcp/internal/identity"
	"github.com/hkjang/confmcp/internal/oauthserver"
	"github.com/hkjang/confmcp/internal/operation"
	"github.com/hkjang/confmcp/internal/permission"
	"github.com/hkjang/confmcp/internal/policy"
	"github.com/hkjang/confmcp/internal/settings"
	"github.com/hkjang/confmcp/internal/tools"
	"github.com/hkjang/confmcp/internal/version"
	"github.com/hkjang/confmcp/internal/webui"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)
	if err := run(); err != nil {
		slog.Error("서비스를 시작할 수 없습니다", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	slog.Info("confmcp 시작", "version", version.Version, "commit", version.Commit,
		"buildDate", version.BuildDate, "addr", cfg.Addr)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		return err
	}
	slog.Info("데이터베이스 마이그레이션 완료")

	sealer, err := crypto.NewSealer(cfg.EncryptionKey)
	if err != nil {
		return err
	}

	store := settings.NewStore(db.Pool, sealer)
	auditLog := audit.New(db.Pool)
	users := auth.NewUsers(db.Pool)
	sessions := auth.NewSessions(db.Pool, sealer)
	keys := apikey.NewService(db.Pool, sealer, store)
	provider := confluence.NewProvider(store, db.Pool)
	resolver := permission.NewResolver(store, provider)
	mapper := identity.NewMapper(db.Pool, store, provider, resolver)
	policies := policy.NewEngine(db.Pool)
	approvals := approval.NewEngine(db.Pool, sealer)
	operations := operation.New(db.Pool)
	uploads := attachment.New(db.Pool)
	oauth := oauthserver.New(db.Pool)
	signer := tools.NewSigner(cfg.EncryptionKey)
	registry := tools.NewRegistry(db.Pool)
	oidc := auth.NewOIDC(store)
	ai := aiproxy.New(store)

	if err := users.EnsureBootstrapAdmin(ctx, cfg.BootstrapAdmin, cfg.BootstrapAdminPasswd); err != nil {
		return err
	}
	if err := keys.SeedRoles(ctx); err != nil {
		return err
	}
	if err := registry.Sync(ctx); err != nil {
		return err
	}
	// A write interrupted by a restart has an unknown outcome.
	if err := operations.MarkAbandoned(ctx, 10*time.Minute); err != nil {
		slog.Warn("중단된 쓰기 작업 점검 실패", "error", err)
	}
	slog.Info("초기화 완료", "bootstrapAdmin", cfg.BootstrapAdmin)

	authSvc := &auth.Service{
		Users: users, Sessions: sessions, OIDC: oidc, Mapper: mapper, OAuth: oauth,
		Keys: keys, Store: store, Audit: auditLog, Sealer: sealer,
	}
	executor := tools.NewExecutor(tools.Deps{
		Registry: registry, Provider: provider, Resolver: resolver, Policy: policies,
		Approvals: approvals, Operations: operations, Uploads: uploads, Audit: auditLog,
		Store: store, Signer: signer,
	})

	server := api.New(api.Deps{
		Pool: db.Pool, Store: store, Auth: authSvc, Users: users, Sessions: sessions,
		Keys: keys, Mapper: mapper, Provider: provider, Resolver: resolver,
		Policy: policies, Approvals: approvals, Registry: registry, Executor: executor,
		Audit: auditLog, AI: ai, OAuth: oauth, Operations: operations, Uploads: uploads,
		Signer: signer, Static: webui.Handler(),
	})

	go housekeeping(ctx, sessions, approvals, auditLog, store, uploads, oauth, operations)

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           server.Router(),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("HTTP 수신 대기", "addr", cfg.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("종료 신호 수신, 정리 중")
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}

// housekeeping performs the periodic maintenance an air-gapped deployment has
// nobody to run by hand.
func housekeeping(ctx context.Context, sessions *auth.Sessions, approvals *approval.Engine,
	auditLog *audit.Logger, store *settings.Store, uploads *attachment.Store, oauth *oauthserver.Server, operations *operation.Store) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := sessions.Cleanup(ctx); err != nil {
				slog.Warn("세션 정리 실패", "error", err)
			}
			if err := approvals.ExpireStale(ctx); err != nil {
				slog.Warn("승인 만료 처리 실패", "error", err)
			}
			limits, _ := store.Limits(ctx)
			if err := approvals.PurgePreviews(ctx, time.Duration(limits.ProposalTTLHours)*time.Hour); err != nil {
				slog.Warn("변경안 본문 정리 실패", "error", err)
			}
			if err := uploads.Purge(ctx); err != nil {
				slog.Warn("업로드 정리 실패", "error", err)
			}
			if err := oauth.Purge(ctx); err != nil {
				slog.Warn("OAuth 토큰 정리 실패", "error", err)
			}
			if err := operations.MarkAbandoned(ctx, 15*time.Minute); err != nil {
				slog.Warn("중단된 쓰기 점검 실패", "error", err)
			}
			if sec, err := store.Security(ctx); err == nil {
				if err := auditLog.Purge(ctx, sec.AuditRetainDays); err != nil {
					slog.Warn("감사 로그 정리 실패", "error", err)
				}
			}
		}
	}
}
