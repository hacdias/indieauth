package indieauth

import (
	"errors"
	"fmt"
	"net/http"
	urlpkg "net/url"
	"strings"
)

var (
	ErrInvalidCodeChallengeMethod error = errors.New("invalid code challenge method")
	ErrInvalidGrantType           error = errors.New("grant_type must be authorization_code")
	ErrNoMatchClientID            error = errors.New("client_id differs")
	ErrNoMatchRedirectURI         error = errors.New("redirect_uri differs")
	ErrPKCERequired               error = errors.New("pkce is required, not provided")
	ErrCodeChallengeFailed        error = errors.New("code challenge failed")
	ErrInvalidResponseType        error = errors.New("response_type must be code")
	ErrWrongCodeChallengeLenght   error = errors.New("code_challenge length must be between 43 and 128 charachters long")
)

type Server struct {
	Client *http.Client

	RequirePKCE bool
}

type AuthenticationRequest struct {
	RedirectURI         string
	ClientID            string
	Scopes              []string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
}

// ParseAuthorization parses an authorization request and returns all the collected
// information about the request.
func (s *Server) ParseAuthorization(r *http.Request) (*AuthenticationRequest, error) {
	if err := r.ParseForm(); err != nil {
		return nil, err
	}

	resType := r.FormValue("response_type")
	if resType == "" {
		// Default to support legacy clients.
		resType = "code"
	}

	if resType != "code" {
		return nil, ErrInvalidResponseType
	}

	clientID := r.FormValue("client_id")
	if err := IsValidClientIdentifier(clientID); err != nil {
		return nil, fmt.Errorf("invalid client_id: %w", err)
	}

	redirectURI := r.FormValue("redirect_uri")
	if err := s.validateRedirectURI(clientID, redirectURI); err != nil {
		return nil, err
	}

	var (
		cc  string
		ccm string
	)

	cc = r.Form.Get("code_challenge")
	if cc != "" {
		if len(cc) < 43 || len(cc) > 128 {
			return nil, ErrWrongCodeChallengeLenght
		}

		ccm = r.Form.Get("code_challenge_method")
		if !IsValidCodeChallengeMethod(ccm) {
			return nil, ErrInvalidCodeChallengeMethod
		}
	} else if s.RequirePKCE {
		return nil, ErrPKCERequired
	}

	req := &AuthenticationRequest{
		RedirectURI:         redirectURI,
		ClientID:            clientID,
		State:               r.Form.Get("state"),
		Scopes:              []string{},
		CodeChallenge:       cc,
		CodeChallengeMethod: ccm,
	}

	scope := r.Form.Get("scope")
	if scope != "" {
		req.Scopes = strings.Split(scope, " ")
	} else if scopes := r.Form["scopes"]; len(scopes) > 0 {
		req.Scopes = scopes
	}

	return req, nil
}

func (s *Server) validateRedirectURI(clientID, redirectURI string) error {
	client, err := urlpkg.Parse(clientID)
	if err != nil {
		return err
	}

	redirect, err := urlpkg.Parse(redirectURI)
	if err != nil {
		return err
	}

	if redirect.Host == client.Host {
		return nil
	}

	// TODO: redirect URI may have a different host. In this case, we do
	// discovery: https://indieauth.spec.indieweb.org/#redirect-url
	return errors.New("redirect uri has different host from client id")
}

// ValidateTokenExchange validates the token exchange request according to the provided
// authentication request and returns the authorization code and an error.
func (s *Server) ValidateTokenExchange(request *AuthenticationRequest, r *http.Request) (string, error) {
	if err := r.ParseForm(); err != nil {
		return "", err
	}

	var (
		grantType = r.Form.Get("grant_type")
		code      = r.Form.Get("code")
	)

	if grantType == "" {
		// Default to support legacy clients.
		grantType = "authorization_code"
	}

	if grantType != "authorization_code" {
		return "", ErrInvalidGrantType
	}

	var (
		clientID     = r.Form.Get("client_id")
		redirectURI  = r.Form.Get("redirect_uri")
		codeVerifier = r.Form.Get("code_verifier")
	)

	if request.ClientID != clientID {
		return "", ErrNoMatchClientID
	}

	if request.RedirectURI != redirectURI {
		return "", ErrNoMatchRedirectURI
	}

	if request.CodeChallenge == "" {
		if s.RequirePKCE {
			return "", ErrPKCERequired
		}
	} else {
		cc := request.CodeChallenge
		ccm := request.CodeChallengeMethod
		if !IsValidCodeChallengeMethod(ccm) {
			return "", ErrInvalidCodeChallengeMethod
		}

		if !ValidateCodeChallenge(ccm, cc, codeVerifier) {
			return "", ErrCodeChallengeFailed
		}
	}

	return code, nil
}
