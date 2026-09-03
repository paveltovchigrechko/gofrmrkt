package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/paveltovchigrechko/gofrmrkt/internal/middleware"
	"github.com/paveltovchigrechko/gofrmrkt/internal/repo"
	"github.com/paveltovchigrechko/gofrmrkt/internal/service"
	"go.uber.org/zap"
)

const authCookieName = "token"

var errEmptyCredentials = errors.New("login or password is empty")

type AppHandler struct {
	db      repo.Storage
	logger  *zap.SugaredLogger
	authSvc *service.AuthService
}

func New(db repo.Storage, logger *zap.SugaredLogger, authSvc *service.AuthService) *AppHandler {
	return &AppHandler{
		db:      db,
		logger:  logger,
		authSvc: authSvc,
	}
}

// --- auth ---

func (h *AppHandler) RegisterUser(w http.ResponseWriter, r *http.Request) {
	if !hasJSONContentType(r) {
		h.fail(w, http.StatusBadRequest, "register: unsupported content type", nil)
		return
	}

	login, password, err := decodeAndValidateUserData(r)
	if err != nil {
		h.fail(w, http.StatusBadRequest, "register: decode request", err)
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

	login, password, err := decodeAndValidateUserData(r)
	if err != nil {
		h.fail(w, http.StatusBadRequest, "login: decode request", err)
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

// --- orders ---

func (h *AppHandler) UploadOrder(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		h.fail(w, http.StatusUnauthorized, "upload order: missing user id in context", nil)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.fail(w, http.StatusBadRequest, "upload order: read body", err)
		return
	}

	orderNumber := strings.TrimSpace(string(body))
	if orderNumber == "" {
		h.fail(w, http.StatusBadRequest, "upload order: empty body", nil)
		return
	}

	if !service.IsValidOrderNumber(orderNumber) {
		h.fail(w, http.StatusUnprocessableEntity, "upload order: invalid order number format", nil)
		return
	}

	err = h.db.UploadOrder(r.Context(), userID, orderNumber)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusAccepted)
	case errors.Is(err, repo.ErrOrderAlreadyUploaded):
		w.WriteHeader(http.StatusOK)
	case errors.Is(err, repo.ErrOrderOwnedByAnotherUser):
		h.fail(w, http.StatusConflict, "upload order: owned by another user", err)
	default:
		h.fail(w, http.StatusInternalServerError, "upload order: persist", err)
	}
}

func (h *AppHandler) GetOrders(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		h.fail(w, http.StatusUnauthorized, "get orders: missing user id in context", nil)
		return
	}

	orders, err := h.db.GetOrders(r.Context(), userID)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "get orders: fetch", err)
		return
	}

	if len(orders) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	h.writeJSON(w, http.StatusOK, orders)
}

// --- balance ---

type balanceResponse struct {
	Current   float64 `json:"current"`
	Withdrawn float64 `json:"withdrawn"`
}

func (h *AppHandler) GetBalance(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		h.fail(w, http.StatusUnauthorized, "get balance: missing user id in context", nil)
		return
	}

	current, withdrawn, err := h.db.GetBalance(r.Context(), userID)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "get balance: fetch", err)
		return
	}

	h.writeJSON(w, http.StatusOK, balanceResponse{Current: current, Withdrawn: withdrawn})
}

type withdrawRequest struct {
	Order string  `json:"order"`
	Sum   float64 `json:"sum"`
}

func (h *AppHandler) WithdrawBalance(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		h.fail(w, http.StatusUnauthorized, "withdraw balance: missing user id in context", nil)
		return
	}

	if !hasJSONContentType(r) {
		h.fail(w, http.StatusBadRequest, "withdraw balance: unsupported content type", nil)
		return
	}

	var req withdrawRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.fail(w, http.StatusBadRequest, "withdraw balance: decode request", err)
		return
	}

	orderNumber := strings.TrimSpace(req.Order)
	if !service.IsValidOrderNumber(orderNumber) {
		h.fail(w, http.StatusUnprocessableEntity, "withdraw balance: invalid order number", nil)
		return
	}

	if req.Sum <= 0 {
		h.fail(w, http.StatusUnprocessableEntity, "withdraw balance: non-positive sum", nil)
		return
	}

	err := h.db.WithdrawBalance(r.Context(), userID, orderNumber, req.Sum)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusOK)
	case errors.Is(err, repo.ErrInsufficientBalance):
		h.fail(w, http.StatusPaymentRequired, "withdraw balance: insufficient funds", err)
	default:
		h.fail(w, http.StatusInternalServerError, "withdraw balance: persist", err)
	}
}

func (h *AppHandler) GetWithdrawals(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		h.fail(w, http.StatusUnauthorized, "get withdrawals: missing user id in context", nil)
		return
	}

	withdrawals, err := h.db.GetWithdrawals(r.Context(), userID)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "get withdrawals: fetch", err)
		return
	}

	if len(withdrawals) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	h.writeJSON(w, http.StatusOK, withdrawals)
}

// --- shared helpers ---

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

func (h *AppHandler) fail(w http.ResponseWriter, status int, msg string, err error) {
	if err != nil {
		h.logger.Errorw(msg, "error", err)
	} else {
		h.logger.Warnw(msg)
	}
	w.WriteHeader(status)
}

func (h *AppHandler) writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		h.logger.Errorw("write json response", "error", err)
	}
}

func hasJSONContentType(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("Content-Type"), "application/json")
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

func decodeAndValidateUserData(r *http.Request) (login, password string, err error) {
	data, err := decodeUserData(r)
	if err != nil {
		return "", "", err
	}

	login = strings.TrimSpace(data.Login)
	password = strings.TrimSpace(data.Password)
	if login == "" || password == "" {
		return "", "", errEmptyCredentials
	}

	return login, password, nil
}
