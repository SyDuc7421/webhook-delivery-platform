package http

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/service"
)

// smallBody bounds request bodies for small JSON control endpoints.
const smallBody = 64 << 10

// ingestBodyLimit bounds bodies that may carry a 256 KB example payload plus JSON overhead.
const ingestBodyLimit = 262144 + 16384

type authHandler struct {
	svc    *service.AuthService
	logger *slog.Logger
}

type registerRequest struct {
	Email    string `json:"email" validate:"required"`
	Password string `json:"password" validate:"required"`
	OrgName  string `json:"org_name" validate:"required"`
}

type loginRequest struct {
	Email    string `json:"email" validate:"required"`
	Password string `json:"password" validate:"required"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

type userResponse struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type orgResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type sessionResponse struct {
	User         userResponse `json:"user"`
	Org          orgResponse  `json:"org"`
	AccessToken  string       `json:"access_token"`
	ExpiresAt    time.Time    `json:"expires_at"`
	RefreshToken string       `json:"refresh_token"`
}

type meResponse struct {
	User userResponse `json:"user"`
	Org  orgResponse  `json:"org"`
}

func toUser(u domain.User) userResponse {
	return userResponse{ID: u.ID.String(), Email: u.Email, Role: string(u.Role), CreatedAt: u.CreatedAt}
}

func toOrg(o domain.Org) orgResponse { return orgResponse{ID: o.ID.String(), Name: o.Name} }

func toSession(s service.Session) sessionResponse {
	return sessionResponse{
		User: toUser(s.User), Org: toOrg(s.Org),
		AccessToken: s.AccessToken, ExpiresAt: s.AccessExpiresAt, RefreshToken: s.RefreshToken,
	}
}

func (h *authHandler) register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := decodeJSON(w, r, &req, smallBody); err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	s, err := h.svc.Register(r.Context(), service.RegisterInput{Email: req.Email, Password: req.Password, OrgName: req.OrgName})
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	respondJSON(w, http.StatusCreated, toSession(s))
}

func (h *authHandler) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(w, r, &req, smallBody); err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	s, err := h.svc.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	respondJSON(w, http.StatusOK, toSession(s))
}

func (h *authHandler) refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := decodeJSON(w, r, &req, smallBody); err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	s, err := h.svc.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	respondJSON(w, http.StatusOK, toSession(s))
}

func (h *authHandler) logout(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := decodeJSON(w, r, &req, smallBody); err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	if err := h.svc.Logout(r.Context(), req.RefreshToken); err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *authHandler) me(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context()) // requireUser guarantees it
	u, o, err := h.svc.Me(r.Context(), p.UserID)
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	respondJSON(w, http.StatusOK, meResponse{User: toUser(u), Org: toOrg(o)})
}
