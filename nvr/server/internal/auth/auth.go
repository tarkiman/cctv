// Package auth implements password login, cookie sessions and request guards.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/tarkiman/cctv/nvr/server/internal/store"
)

const (
	CookieName = "nvr_session"
	sessionTTL = 30 * 24 * time.Hour
	// Sessions are extended at most this often, to avoid a write on every request.
	touchEvery = 10 * time.Minute

	maxFailures   = 5
	failureWindow = time.Minute

	// CSRFHeader must accompany state-changing requests. Browsers cannot send custom
	// headers cross-site without a CORS preflight, which this server never approves.
	CSRFHeader = "X-Requested-With"
	CSRFValue  = "nvr"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrRateLimited        = errors.New("too many attempts")
)

type Service struct {
	Store        *store.Store
	SecureCookie bool

	mu       sync.Mutex
	failures map[string][]time.Time
}

func New(s *store.Store, secureCookie bool) *Service {
	return &Service{Store: s, SecureCookie: secureCookie, failures: map[string][]time.Time{}}
}

var dummyHash = sync.OnceValue(func() []byte {
	h, _ := bcrypt.GenerateFromPassword([]byte("not-a-real-password"), bcrypt.DefaultCost)
	return h
})

func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

// Bootstrap creates the first admin from the given credentials when no user exists.
// It returns true when a user was created.
func (s *Service) Bootstrap(ctx context.Context, username, password string) (bool, error) {
	n, err := s.Store.Users.Count(ctx)
	if err != nil || n > 0 || username == "" || password == "" {
		return false, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return false, err
	}
	_, err = s.Store.Users.Create(ctx, username, hash, "admin")
	return err == nil, err
}

// RetryAfter returns how long the IP must wait before trying again, or 0.
func (s *Service) RetryAfter(ip string) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	recent := s.prune(ip)
	if len(recent) < maxFailures {
		return 0
	}
	return time.Until(recent[0].Add(failureWindow)).Round(time.Second)
}

func (s *Service) prune(ip string) []time.Time {
	cutoff := time.Now().Add(-failureWindow)
	recent := s.failures[ip][:0]
	for _, t := range s.failures[ip] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	if len(recent) == 0 {
		delete(s.failures, ip)
		return nil
	}
	s.failures[ip] = recent
	return recent
}

func (s *Service) fail(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures[ip] = append(s.prune(ip), time.Now())
}

// Verify checks a username/password pair, counting failures toward the per-IP limit.
func (s *Service) Verify(ctx context.Context, ip, username, password string) (*store.User, error) {
	if s.RetryAfter(ip) > 0 {
		return nil, ErrRateLimited
	}
	u, err := s.Store.Users.ByUsername(ctx, username)
	if errors.Is(err, store.ErrNotFound) {
		// Spend the same time as a real check so usernames can't be probed by timing.
		bcrypt.CompareHashAndPassword(dummyHash(), []byte(password))
		s.fail(ip)
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		s.fail(ip)
		return nil, ErrInvalidCredentials
	}
	return u, nil
}

// Login checks credentials and, on success, sets the session cookie.
func (s *Service) Login(ctx context.Context, w http.ResponseWriter, r *http.Request, username, password string) (*store.User, error) {
	ip := ClientIP(r)
	u, err := s.Verify(ctx, ip, username, password)
	if err != nil {
		return nil, err
	}
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	expires := time.Now().Add(sessionTTL)
	if err := s.Store.Sessions.Create(ctx, hashToken(token), u.ID, expires.Unix(), r.UserAgent(), ip); err != nil {
		return nil, err
	}
	s.setCookie(w, token, expires)
	return u, nil
}

func (s *Service) Logout(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(CookieName); err == nil {
		_ = s.Store.Sessions.Delete(ctx, hashToken(c.Value))
	}
	s.setCookie(w, "", time.Unix(0, 0))
}

func (s *Service) setCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   s.SecureCookie,
		SameSite: http.SameSiteLaxMode,
	})
}

type ctxKey struct{}

type Identity struct {
	User      *store.User
	SessionID string
}

// FromContext returns the authenticated identity set by Require.
func FromContext(ctx context.Context) *Identity {
	id, _ := ctx.Value(ctxKey{}).(*Identity)
	return id
}

// Authenticate resolves the session cookie to a user, extending the session when due.
func (s *Service) Authenticate(r *http.Request) (*Identity, error) {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return nil, store.ErrNotFound
	}
	id := hashToken(c.Value)
	sess, err := s.Store.Sessions.Valid(r.Context(), id)
	if err != nil {
		return nil, err
	}
	u, err := s.Store.Users.ByID(r.Context(), sess.UserID)
	if err != nil {
		return nil, err
	}
	if time.Since(time.Unix(sess.LastSeen, 0)) > touchEvery {
		_ = s.Store.Sessions.Touch(r.Context(), id, time.Now().Add(sessionTTL).Unix())
	}
	return &Identity{User: u, SessionID: id}, nil
}

// Require rejects requests without a valid session; onFail writes the response.
func (s *Service) Require(onFail func(w http.ResponseWriter, r *http.Request)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ident, err := s.Authenticate(r)
			if err != nil {
				onFail(w, r)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, ident)))
		})
	}
}

// CSRF rejects state-changing requests that lack the custom header.
func CSRF(onFail func(w http.ResponseWriter, r *http.Request)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
			default:
				if r.Header.Get(CSRFHeader) != CSRFValue {
					onFail(w, r)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ClientIP is the remote address without port. RealIP middleware (if trusted) has
// already rewritten RemoteAddr.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
