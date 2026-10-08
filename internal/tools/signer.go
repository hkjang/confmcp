package tools

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Signer issues tamper-proof tokens bound to one requester: search cursors
// and short-lived attachment download links. A token minted for one user is
// useless to another, and a cursor stops working when the policy changes.
type Signer struct{ key []byte }

// NewSigner derives a signing key from the service encryption key.
func NewSigner(master []byte) *Signer {
	mac := hmac.New(sha256.New, master)
	mac.Write([]byte("confmcp/token-signing/v1"))
	return &Signer{key: mac.Sum(nil)}
}

// ErrBadToken means a cursor or link is forged, expired or not the caller's.
var ErrBadToken = errors.New("유효하지 않거나 만료된 커서/링크입니다")

// Seal signs a payload.
func (s *Signer) Seal(v any) (string, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, s.key)
	mac.Write(body)
	return base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// Open verifies a token and decodes its payload.
func (s *Signer) Open(token string, out any) error {
	body64, sig64, ok := strings.Cut(token, ".")
	if !ok {
		return ErrBadToken
	}
	body, err := base64.RawURLEncoding.DecodeString(body64)
	if err != nil {
		return ErrBadToken
	}
	sig, err := base64.RawURLEncoding.DecodeString(sig64)
	if err != nil {
		return ErrBadToken
	}
	mac := hmac.New(sha256.New, s.key)
	mac.Write(body)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return ErrBadToken
	}
	if err := json.Unmarshal(body, out); err != nil {
		return ErrBadToken
	}
	return nil
}

// cursor is the signed state of a paged listing or search.
type cursor struct {
	Sub    string `json:"s"`
	Query  string `json:"q"`
	Gen    int64  `json:"g"`
	Offset int    `json:"o"`
	Exp    int64  `json:"e"`
}

func (s *Signer) newCursor(sub, query string, gen int64, offset int) string {
	t, _ := s.Seal(cursor{Sub: sub, Query: query, Gen: gen, Offset: offset, Exp: time.Now().Add(2 * time.Hour).Unix()})
	return t
}

func (s *Signer) openCursor(token, sub, query string, gen int64) (int, error) {
	if token == "" {
		return 0, nil
	}
	var c cursor
	if err := s.Open(token, &c); err != nil {
		return 0, err
	}
	if c.Sub != sub || c.Query != query || time.Now().Unix() > c.Exp {
		return 0, ErrBadToken
	}
	if c.Gen != gen {
		return 0, errors.New("접근 정책이 바뀌어 커서를 사용할 수 없습니다. 처음부터 다시 조회하십시오")
	}
	return c.Offset, nil
}

// DownloadToken is a short-lived link to fetch one attachment as one user.
type DownloadToken struct {
	Sub          string `json:"s"`
	AttachmentID string `json:"a"`
	Exp          int64  `json:"e"`
}

// NewDownload issues a download token valid for ttl.
func (s *Signer) NewDownload(sub, attachmentID string, ttl time.Duration) (string, time.Time) {
	exp := time.Now().Add(ttl)
	t, _ := s.Seal(DownloadToken{Sub: sub, AttachmentID: attachmentID, Exp: exp.Unix()})
	return t, exp
}

// OpenDownload verifies a download token.
func (s *Signer) OpenDownload(token string) (*DownloadToken, error) {
	var d DownloadToken
	if err := s.Open(token, &d); err != nil {
		return nil, err
	}
	if time.Now().Unix() > d.Exp {
		return nil, ErrBadToken
	}
	return &d, nil
}
