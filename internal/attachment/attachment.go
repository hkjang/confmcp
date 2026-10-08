// Package attachment holds files uploaded ahead of a Confluence attachment
// write. A tool only ever receives the upload's ID: never a server path and
// never a URL to fetch, so a model cannot make the gateway read local files or
// reach arbitrary hosts.
package attachment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound means the upload does not exist, is not the caller's, expired or
// was already used.
var ErrNotFound = errors.New("업로드를 찾을 수 없습니다 (만료·사용됨·다른 사용자)")

// Upload is a stored file awaiting attachment.
type Upload struct {
	ID        uuid.UUID  `json:"uploadId"`
	UserID    int64      `json:"-"`
	Filename  string     `json:"filename"`
	MediaType string     `json:"mediaType"`
	Size      int64      `json:"size"`
	SHA256    string     `json:"sha256"`
	Data      []byte     `json:"-"`
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt time.Time  `json:"expiresAt"`
	Consumed  *time.Time `json:"consumedAt,omitempty"`
}

// Store persists uploads.
type Store struct{ pool *pgxpool.Pool }

// New builds the store.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// blockedExt are refused outright: executables and scripts have no business
// travelling through a documentation gateway.
var blockedExt = map[string]bool{
	".exe": true, ".dll": true, ".bat": true, ".cmd": true, ".com": true, ".scr": true, ".msi": true,
	".ps1": true, ".vbs": true, ".js": true, ".jar": true, ".sh": true, ".hta": true, ".lnk": true,
}

// CleanFilename validates and normalises an attachment file name.
func CleanFilename(name string) (string, error) {
	name = strings.TrimSpace(filepath.Base(strings.ReplaceAll(name, "\\", "/")))
	if name == "" || name == "." || name == "/" {
		return "", errors.New("파일 이름이 필요합니다")
	}
	if !utf8.ValidString(name) || strings.ContainsAny(name, "\x00\r\n<>:\"|?*") {
		return "", errors.New("파일 이름에 사용할 수 없는 문자가 있습니다")
	}
	if utf8.RuneCountInString(name) > 200 {
		return "", errors.New("파일 이름이 너무 깁니다 (200자 이하)")
	}
	if blockedExt[strings.ToLower(filepath.Ext(name))] {
		return "", fmt.Errorf("허용되지 않는 파일 형식입니다 (%s)", filepath.Ext(name))
	}
	return name, nil
}

// Save stores a file after validating its name, size and type.
func (s *Store) Save(ctx context.Context, userID int64, filename, declaredType string, data []byte, maxBytes int64, ttl time.Duration) (*Upload, error) {
	name, err := CleanFilename(filename)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, errors.New("빈 파일입니다")
	}
	if maxBytes > 0 && int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("LIMIT_EXCEEDED: 파일 크기 %d 바이트가 한도 %d 바이트를 넘습니다", len(data), maxBytes)
	}
	sniffed := http.DetectContentType(data)
	mediaType := sniffed
	if ext := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); ext != "" {
		mediaType = ext
	}
	if strings.HasPrefix(sniffed, "application/x-msdownload") || strings.Contains(sniffed, "x-executable") {
		return nil, errors.New("실행 파일은 첨부할 수 없습니다")
	}
	_ = declaredType
	sum := sha256.Sum256(data)
	u := &Upload{ID: uuid.New(), UserID: userID, Filename: name, MediaType: mediaType, Size: int64(len(data)),
		SHA256: hex.EncodeToString(sum[:]), Data: data, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(ttl)}
	_, err = s.pool.Exec(ctx, `INSERT INTO attachment_uploads(id, user_id, filename, media_type, size_bytes, sha256, data, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, u.ID, userID, u.Filename, u.MediaType, u.Size, u.SHA256, data, u.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

// Get loads an unexpired, unconsumed upload owned by userID.
func (s *Store) Get(ctx context.Context, userID int64, id uuid.UUID, withData bool) (*Upload, error) {
	u := &Upload{}
	dataCol := "NULL::BYTEA"
	if withData {
		dataCol = "data"
	}
	err := s.pool.QueryRow(ctx, `SELECT id, user_id, filename, media_type, size_bytes, sha256, `+dataCol+`,
		created_at, expires_at, consumed_at FROM attachment_uploads
		WHERE id=$1 AND user_id=$2 AND consumed_at IS NULL AND expires_at > NOW()`, id, userID).
		Scan(&u.ID, &u.UserID, &u.Filename, &u.MediaType, &u.Size, &u.SHA256, &u.Data, &u.CreatedAt, &u.ExpiresAt, &u.Consumed)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if withData {
		sum := sha256.Sum256(u.Data)
		if hex.EncodeToString(sum[:]) != u.SHA256 {
			return nil, errors.New("업로드 파일의 해시가 기록과 다릅니다")
		}
	}
	return u, nil
}

// Consume marks an upload used and drops its bytes.
func (s *Store) Consume(ctx context.Context, id uuid.UUID) {
	_, _ = s.pool.Exec(context.WithoutCancel(ctx),
		`UPDATE attachment_uploads SET consumed_at=NOW(), data='' WHERE id=$1`, id)
}

// List returns a user's recent uploads without their bytes.
func (s *Store) List(ctx context.Context, userID int64) ([]Upload, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, user_id, filename, media_type, size_bytes, sha256, created_at, expires_at, consumed_at
		FROM attachment_uploads WHERE user_id=$1 ORDER BY created_at DESC LIMIT 50`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Upload{}
	for rows.Next() {
		var u Upload
		if err := rows.Scan(&u.ID, &u.UserID, &u.Filename, &u.MediaType, &u.Size, &u.SHA256, &u.CreatedAt, &u.ExpiresAt, &u.Consumed); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// Delete removes an upload owned by userID.
func (s *Store) Delete(ctx context.Context, userID int64, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM attachment_uploads WHERE id=$1 AND user_id=$2`, id, userID)
	return err
}

// Purge removes expired and consumed uploads.
func (s *Store) Purge(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM attachment_uploads
		WHERE expires_at < NOW() OR (consumed_at IS NOT NULL AND consumed_at < NOW() - INTERVAL '1 day')`)
	return err
}
