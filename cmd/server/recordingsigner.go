package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// recordingSigner mints and validates HMAC-signed, time-bounded URLs for
// downloading call recordings. The signing key is loaded from
// RECORDING_SIGNING_KEY when present, otherwise persisted to
// media/.recording-key so links survive process restarts.
type recordingSigner struct{ key []byte }

const recordingKeyPath = "media/.recording-key"

func newRecordingSigner() (*recordingSigner, error) {
	if env := os.Getenv("RECORDING_SIGNING_KEY"); len(env) >= 16 {
		return &recordingSigner{key: []byte(env)}, nil
	}
	if b, err := os.ReadFile(recordingKeyPath); err == nil && len(b) >= 16 {
		return &recordingSigner{key: b}, nil
	}
	if err := os.MkdirAll(filepath.Dir(recordingKeyPath), 0o755); err != nil {
		return nil, err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	if err := os.WriteFile(recordingKeyPath, b, 0o600); err != nil {
		return nil, err
	}
	return &recordingSigner{key: b}, nil
}

// Sign returns (sig, expUnix) for a token and ttl.
func (s *recordingSigner) Sign(token string, ttl time.Duration, download bool) (string, int64) {
	exp := time.Now().Add(ttl).Unix()
	return s.sigFor(token, exp, download), exp
}

func (s *recordingSigner) sigFor(token string, exp int64, download bool) string {
	mac := hmac.New(sha256.New, s.key)
	d := "0"
	if download {
		d = "1"
	}
	fmt.Fprintf(mac, "%s|%d|%s", token, exp, d)
	return hex.EncodeToString(mac.Sum(nil))
}

// Verify checks sig+exp against token. Returns nil on success.
func (s *recordingSigner) Verify(token, sig string, exp int64, download bool) error {
	if exp <= 0 {
		return errors.New("missing exp")
	}
	if time.Now().Unix() > exp {
		return errors.New("expired")
	}
	want := s.sigFor(token, exp, download)
	if !hmac.Equal([]byte(want), []byte(sig)) {
		return errors.New("bad signature")
	}
	return nil
}

func parseExp(v string) int64 {
	n, _ := strconv.ParseInt(v, 10, 64)
	return n
}
