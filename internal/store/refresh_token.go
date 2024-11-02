package store

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Token struct {
	CSRFToken string `json:"CSRFPreventionToken"`
	Ticket    string `json:"ticket"`
	Username  string `json:"username"`
}

type TokenResponse struct {
	Data Token `json:"data"`
}

type TokenRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type APITokenRequest struct {
	Comment string `json:"comment"`
}

type APITokenResponse struct {
	Data APIToken `json:"data"`
}

type APIToken struct {
	TokenId string `json:"tokenid"`
	Value   string `json:"value"`
}

func (token *Token) CreateAPIToken() (*APIToken, error) {
	authCookie := token.Ticket
	decodedAuthCookie := strings.ReplaceAll(authCookie, "%3A", ":")

	authCookieParts := strings.Split(decodedAuthCookie, ":")
	if len(authCookieParts) < 5 {
		return nil, fmt.Errorf("CreateAPIToken: invalid cookie %s", authCookie)
	}

	username := authCookieParts[1]

	client := http.Client{
		Timeout: time.Second * 10,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	existingReq, err := http.NewRequest(
		http.MethodDelete,
		fmt.Sprintf(
			"%s/api2/json/access/users/%s/token/pbs-d2d-auth",
			ProxyTargetURL,
			username,
		),
		nil,
	)
	existingReq.AddCookie(&http.Cookie{
		Path:  "/",
		Name:  "PBSAuthCookie",
		Value: authCookie,
	})
	existingReq.Header.Add("Csrfpreventiontoken", token.CSRFToken)
	existingReq.Header.Add("Content-Type", "application/json")
	_, _ = client.Do(existingReq)

	reqBody, err := json.Marshal(&APITokenRequest{
		Comment: "Autogenerated API token for PBS-D2D Addon",
	})

	tokensReq, err := http.NewRequest(
		http.MethodPost,
		fmt.Sprintf(
			"%s/api2/json/access/users/%s/token/pbs-d2d-auth",
			ProxyTargetURL,
			username,
		),
		bytes.NewBuffer(reqBody),
	)
	tokensReq.AddCookie(&http.Cookie{
		Path:  "/",
		Name:  "PBSAuthCookie",
		Value: authCookie,
	})
	tokensReq.Header.Add("Csrfpreventiontoken", token.CSRFToken)
	tokensReq.Header.Add("Content-Type", "application/json")

	tokensResp, err := client.Do(tokensReq)
	if err != nil {
		return nil, fmt.Errorf("CreateAPIToken: error executing http request -> %w", err)
	}

	tokensBody, err := io.ReadAll(tokensResp.Body)
	if err != nil {
		return nil, fmt.Errorf("CreateAPIToken: error reading response body -> %w", err)
	}

	var tokenStruct APITokenResponse
	err = json.Unmarshal(tokensBody, &tokenStruct)
	if err != nil {
		return nil, fmt.Errorf("CreateAPIToken: error json unmarshal body -> %w", err)
	}

	return &tokenStruct.Data, nil
}

func (token *APIToken) SaveToFile() error {
	if token == nil {
		return nil
	}

	tokenFileContent, _ := json.Marshal(token)
	file, err := os.OpenFile(filepath.Join(DbBasePath, "pbs-d2d-token.json"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.WriteString(string(tokenFileContent))
	if err != nil {
		return err
	}

	return nil
}

func GetAPITokenFromFile() (*APIToken, error) {
	jsonFile, err := os.Open(filepath.Join(DbBasePath, "pbs-d2d-token.json"))
	if err != nil {
		return nil, err
	}
	defer jsonFile.Close()

	byteValue, err := io.ReadAll(jsonFile)
	if err != nil {
		return nil, err
	}

	var result APIToken
	err = json.Unmarshal([]byte(byteValue), &result)
	if err != nil {
		return nil, err
	}

	return &result, nil
}
