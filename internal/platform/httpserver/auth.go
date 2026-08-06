package httpserver

import (
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"campaign-platform/internal/identity"
	"campaign-platform/internal/shared/httpx"
)

const sessionCookieName = "campaign_session"

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type verifyMFARequest struct {
	ChallengeToken string `json:"challengeToken"`
	Code           string `json:"code"`
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var input loginRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The login request is invalid.", nil)
		return
	}
	result, err := s.deps.Identity.LoginWithContext(r.Context(), input.Email, input.Password, authenticationAttempt(r, input.Email))
	if err != nil {
		status := http.StatusUnauthorized
		if errors.Is(err, identity.ErrAccountLocked) {
			status = http.StatusTooManyRequests
		}
		httpx.WriteError(w, r, status, "AUTHENTICATION_FAILED", "The credentials could not be verified.", nil)
		return
	}
	if result.SessionToken != "" {
		s.setSessionCookie(w, result.SessionToken, result.ExpiresAt)
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (s *Server) verifyMFA(w http.ResponseWriter, r *http.Request) {
	var input verifyMFARequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The MFA request is invalid.", nil)
		return
	}
	result, err := s.deps.Identity.VerifyMFAWithContext(r.Context(), input.ChallengeToken, input.Code, authenticationAttempt(r, ""))
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnauthorized, "MFA_FAILED", "The multi-factor code could not be verified.", nil)
		return
	}
	s.setSessionCookie(w, result.SessionToken, result.ExpiresAt)
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	token := bearerOrCookie(r)
	if token != "" {
		s.deps.Identity.Revoke(token)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.deps.SecureCookies, SameSite: http.SameSiteStrictMode})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	principal, ok := identity.PrincipalFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "Authentication is required.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"id": principal.User.ID, "email": principal.User.Email, "displayName": principal.User.DisplayName,
		"permissions": principal.User.PermissionList(), "sessionId": principal.SessionID,
		"lastLoginAt": principal.User.LastLoginAt,
	})
}

func (s *Server) require(permission string, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.deps.Identity == nil {
			httpx.WriteError(w, r, http.StatusServiceUnavailable, "IDENTITY_UNAVAILABLE", "Identity services are unavailable.", nil)
			return
		}
		token := bearerOrCookie(r)
		if token == "" {
			httpx.WriteError(w, r, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "Authentication is required.", nil)
			return
		}
		user, session, err := s.deps.Identity.Authenticate(r.Context(), token)
		if err != nil {
			httpx.WriteError(w, r, http.StatusUnauthorized, "SESSION_INVALID", "The session is invalid or expired.", nil)
			return
		}
		if permission != "" && !user.HasPermission(permission) {
			s.deps.Identity.RecordPermissionDenied(r.Context(), user.ID, permission, authenticationAttempt(r, user.Email))
			httpx.WriteError(w, r, http.StatusForbidden, "PERMISSION_DENIED", "The user is not authorised for this action.", nil)
			return
		}
		// Cookie-authenticated unsafe requests require a CSRF token. Bearer tokens are not
		// ambient browser credentials and therefore do not require this additional check.
		if r.Header.Get("Authorization") == "" && isUnsafeMethod(r.Method) && !identity.CSRFTokenValid(session, r.Header.Get("X-CSRF-Token")) {
			httpx.WriteError(w, r, http.StatusForbidden, "CSRF_VALIDATION_FAILED", "The request could not be verified.", nil)
			return
		}
		ctx := identity.WithPrincipal(r.Context(), identity.Principal{User: user, Session: session, SessionID: session.ID, CSRFToken: r.Header.Get("X-CSRF-Token")})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func bearerOrCookie(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get("Authorization")); strings.HasPrefix(strings.ToLower(value), "bearer ") {
		return strings.TrimSpace(value[7:])
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil {
		return strings.TrimSpace(cookie.Value)
	}
	return ""
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: token, Path: "/", Expires: expiresAt,
		HttpOnly: true, Secure: s.deps.SecureCookies, SameSite: http.SameSiteStrictMode})
}

func isUnsafeMethod(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}

func constantStringEqual(left, right string) bool {
	if len(left) != len(right) || len(left) == 0 {
		return false
	}
	var diff byte
	for i := range left {
		diff |= left[i] ^ right[i]
	}
	return diff == 0
}

type stepUpRequest struct {
	Code string `json:"code"`
}

func (s *Server) stepUp(w http.ResponseWriter, r *http.Request) {
	var input stepUpRequest
	if err := httpx.DecodeJSON(w, r, 32<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The step-up request is invalid.", nil)
		return
	}
	token := bearerOrCookie(r)
	session, err := s.deps.Identity.StepUp(r.Context(), token, input.Code, authenticationAttempt(r, ""))
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnauthorized, "MFA_FAILED", "The multi-factor code could not be verified.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"sessionId": session.ID, "mfaVerifiedAt": session.MFAVerifiedAt})
}

func (s *Server) activeSessions(w http.ResponseWriter, r *http.Request) {
	principal, ok := identity.PrincipalFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "Authentication is required.", nil)
		return
	}
	items, err := s.deps.Identity.ActiveSessionsContext(r.Context(), principal.User.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteListAuto(w, http.StatusOK, items)
}

func (s *Server) revokeAllSessions(w http.ResponseWriter, r *http.Request) {
	principal, ok := identity.PrincipalFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "Authentication is required.", nil)
		return
	}
	count, err := s.deps.Identity.RevokeAllContext(r.Context(), principal.User.ID, "user requested revoke all")
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.deps.SecureCookies, SameSite: http.SameSiteStrictMode})
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"revoked": count})
}

func authenticationAttempt(r *http.Request, email string) identity.AttemptContext {
	ip := strings.TrimSpace(r.RemoteAddr)
	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}
	return identity.AttemptContext{Email: email, SourceIP: ip, UserAgent: r.UserAgent(), CorrelationID: httpx.RequestID(r.Context())}
}
