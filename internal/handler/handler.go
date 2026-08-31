package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/paveltovchigrechko/gofrmrkt/internal/repo"
	"github.com/paveltovchigrechko/gofrmrkt/internal/service"
	"go.uber.org/zap"
)

const (
	authCookieName = "token"
)

type AppHandler struct {
	db      *repo.Postgres
	logger  *zap.SugaredLogger
	authSvc *service.AuthService
}

func New(db *repo.Postgres, logger *zap.SugaredLogger, authSvc *service.AuthService) *AppHandler {
	return &AppHandler{
		db:      db,
		logger:  logger,
		authSvc: authSvc,
	}
}

func (h *AppHandler) RegisterUser(w http.ResponseWriter, r *http.Request) {
	if !hasJSONContentType(r) {
		h.fail(w, http.StatusBadRequest, "register: unsupported content type", nil)
		return
	}

	data, err := decodeUserData(r)
	if err != nil {
		h.fail(w, http.StatusBadRequest, "register: decode request", err)
		return
	}

	login := strings.TrimSpace(data.Login)
	password := strings.TrimSpace(data.Password)
	if login == "" || password == "" {
		h.fail(w, http.StatusBadRequest, "register: empty login or password", nil)
		return
	}

	hashedPassword, err := service.HashPassword(password)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "register: hash password", err)
		return
	}

	userID, err := h.db.RegisterUser(r.Context(), login, hashedPassword)
	if err != nil {
		switch {
		case errors.Is(err, repo.ErrUserLoginExist):
			h.fail(w, http.StatusConflict, "register: login already exists", err)
		default:
			h.fail(w, http.StatusInternalServerError, "register: persist user", err)
		}
		return
	}

	if err := h.authenticate(w, userID); err != nil {
		h.fail(w, http.StatusInternalServerError, "register: issue token", err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *AppHandler) AuthenticateUser(w http.ResponseWriter, r *http.Request) {
	if !hasJSONContentType(r) {
		h.fail(w, http.StatusBadRequest, "login: unsupported content type", nil)
		return
	}

	data, err := decodeUserData(r)
	if err != nil {
		h.fail(w, http.StatusBadRequest, "login: decode request", err)
		return
	}

	login := strings.TrimSpace(data.Login)
	password := strings.TrimSpace(data.Password)
	if login == "" || password == "" {
		h.fail(w, http.StatusBadRequest, "login: empty login or password", nil)
		return
	}

	userID, passwordHash, err := h.db.GetUserByLogin(r.Context(), login)
	if err != nil {
		switch {
		case errors.Is(err, repo.ErrUserNotFound):
			h.fail(w, http.StatusUnauthorized, "login: unknown login", nil)
		default:
			h.fail(w, http.StatusInternalServerError, "login: fetch user", err)
		}
		return
	}

	if !service.CheckPasswordHash(password, passwordHash) {
		h.fail(w, http.StatusUnauthorized, "login: password mismatch", nil)
		return
	}

	if err := h.authenticate(w, userID); err != nil {
		h.fail(w, http.StatusInternalServerError, "login: issue token", err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// authenticate builds a JWT for userID and sets it as the auth cookie.
func (h *AppHandler) authenticate(w http.ResponseWriter, userID int64) error {
	token, err := h.authSvc.BuildJWTString(userID)
	if err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
	})

	return nil
}

// fail logs the error (if any) with context and writes the status code.
func (h *AppHandler) fail(w http.ResponseWriter, status int, msg string, err error) {
	if err != nil {
		h.logger.Errorw(msg, "error", err)
	} else {
		h.logger.Warnw(msg)
	}
	w.WriteHeader(status)
}

func hasJSONContentType(r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	return strings.HasPrefix(ct, "application/json")
}

type userData struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

func decodeUserData(r *http.Request) (*userData, error) {
	var result userData
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}
