package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type AppHandler struct {
}

func New() *AppHandler {
	return &AppHandler{}
}

func (h *AppHandler) RegisterUser(w http.ResponseWriter, r *http.Request) {
	// TODO: validate request type and content type
	data, err := decodeUserData(r)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Println(err)
		return
	}
	fmt.Printf("login: %s\npassword: %s\n", data.Login, data.Password)
}

func (h *AppHandler) AuthenticateUser(w http.ResponseWriter, r *http.Request) {

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
