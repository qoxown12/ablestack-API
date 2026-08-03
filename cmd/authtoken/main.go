package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"ablecloud.io/ablestack-api/internal/service/authservice"
)

type tokenResponse struct {
	Code          int    `json:"code"`
	TokenType     string `json:"token_type"`
	AccessToken   string `json:"access_token"`
	Authorization string `json:"authorization"`
	ExpiresIn     int64  `json:"expires_in"`
	Subject       string `json:"subject"`
}

func main() {
	plain := flag.Bool("plain", false, "print only the Authorization header value")
	username := flag.String("user", "", "Linux user to issue a token for; root-only when different from the current user")
	flag.Parse()

	subject, err := resolveSubject(*username)
	if err != nil {
		exitError(err)
	}
	token, err := authservice.IssueAccessTokenForLinuxUser(subject)
	if err != nil {
		exitError(err)
	}
	if *plain {
		fmt.Println(token.Authorization)
		return
	}
	resp := tokenResponse{
		Code:          200,
		TokenType:     token.TokenType,
		AccessToken:   token.AccessToken,
		Authorization: token.Authorization,
		ExpiresIn:     token.ExpiresIn,
		Subject:       token.Subject,
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		exitError(err)
	}
	fmt.Println(string(raw))
}

func resolveSubject(requested string) (string, error) {
	current, err := authservice.CurrentLinuxUsername()
	if err != nil {
		return "", err
	}
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return current, nil
	}
	if !authservice.IsSafeLinuxUsername(requested) {
		return "", fmt.Errorf("invalid linux user")
	}
	if requested != current && os.Geteuid() != 0 {
		return "", fmt.Errorf("--user can only target another account when running as root")
	}
	return requested, nil
}

func exitError(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
