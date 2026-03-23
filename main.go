package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

//go:embed static/*
var staticFiles embed.FS

type Server struct {
	baseURL    string
	defaultURL string
	sessionID  string
	mu         sync.RWMutex

	probeClient    *http.Client
	exchangeClient *http.Client
}

func main() {
	port := flag.Int("port", 8080, "Port to run the server on")
	env := flag.String("env", "gummi-dos", "Environment name")
	tenant := flag.String("tenant", "arasaka", "Tenant code")
	flag.Parse()

	defaultURL := fmt.Sprintf("https://api.%s.%s.saas.cmddev.thermofisher.com", *tenant, *env)
	s := &Server{
		baseURL:    fmt.Sprintf("http://localhost:%d", *port),
		defaultURL: defaultURL,
		probeClient: &http.Client{
			Timeout: 5 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		exchangeClient: &http.Client{Timeout: 10 * time.Second},
	}

	http.HandleFunc("/", s.handleIndex)
	http.HandleFunc("/config", s.handleConfig)
	http.HandleFunc("/login", s.handleLogin)
	http.HandleFunc("/logout", s.handleLogout)
	http.HandleFunc("/callback", s.handleCallback)
	http.HandleFunc("/session", s.handleGetSession)
	http.HandleFunc("/keep-alive", s.handleKeepAlive)

	log.Printf("Auth server running at %s", s.baseURL)
	log.Printf("Open this URL in your browser to authenticate")
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), nil))
}

// deriveAuthURL converts an API Gateway URL to the hosted login app URL.
//
//	API:  https://api.{tenant}.{env}.saas.cmddev.thermofisher.com
//	Auth: https://{env}.{tenant}.auth.saas.cmddev.thermofisher.com
func deriveAuthURL(apiURL string) (string, error) {
	u, err := url.Parse(apiURL)
	if err != nil {
		return "", err
	}

	parts := strings.Split(u.Hostname(), ".")
	// Expected: [api, tenant, env, saas, cmddev, thermofisher, com]
	if len(parts) < 7 || parts[0] != "api" {
		return "", fmt.Errorf("unexpected API URL format (expected api.tenant.env.saas.*.*.*): %s", apiURL)
	}

	tenant := parts[1]
	env := parts[2]
	rest := strings.Join(parts[3:], ".")

	return fmt.Sprintf("https://%s.%s.auth.%s", env, tenant, rest), nil
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	data, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "Failed to load index.html", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"default_api_url": s.defaultURL})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	apiURL := r.URL.Query().Get("api_url")
	if apiURL == "" {
		http.Error(w, "Missing api_url parameter", http.StatusBadRequest)
		return
	}

	callbackURL := s.baseURL + "/callback"

	// Probe legacy endpoint — don't follow redirects so we can inspect the status code
	oldLoginURL := fmt.Sprintf("%s/user/login?redirect_uri=%s", apiURL, url.QueryEscape(callbackURL))
	resp, err := s.probeClient.Get(oldLoginURL)
	if err != nil {
		log.Printf("Legacy endpoint probe failed: %v, trying hosted login", err)
	} else {
		resp.Body.Close()
		if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusTemporaryRedirect {
			log.Printf("Using legacy login flow: %s", oldLoginURL)
			http.Redirect(w, r, oldLoginURL, http.StatusFound)
			return
		}
	}

	authURL, err := deriveAuthURL(apiURL)
	if err != nil {
		log.Printf("ERROR: Failed to derive auth URL: %v", err)
		http.Error(w, fmt.Sprintf("Failed to derive auth URL: %v", err), http.StatusInternalServerError)
		return
	}

	// Encode auth_url in the callback so the callback handler is self-contained
	// (no shared state needed between login and callback).
	redirectURI := fmt.Sprintf("%s?auth_url=%s", callbackURL, url.QueryEscape(authURL))
	loginURL := fmt.Sprintf("%s/?redirect_uri=%s", authURL, url.QueryEscape(redirectURI))
	log.Printf("Using hosted login flow: %s", loginURL)
	http.Redirect(w, r, loginURL, http.StatusFound)
}

func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var sessionID string
	var flow string

	switch {
	case q.Get("session_id") != "":
		sessionID = q.Get("session_id")
		flow = "legacy"

	case q.Get("code") != "":
		authURL := q.Get("auth_url")
		if authURL == "" {
			log.Printf("ERROR: Callback has code but no auth_url")
			http.Error(w, "Missing auth_url in callback", http.StatusBadRequest)
			return
		}
		var err error
		sessionID, err = s.exchangeCode(authURL, q.Get("code"))
		if err != nil {
			log.Printf("ERROR: Code exchange failed: %v", err)
			http.Error(w, fmt.Sprintf("Code exchange failed: %v", err), http.StatusInternalServerError)
			return
		}
		flow = "hosted login"

	default:
		log.Printf("ERROR: Callback received without session_id or code")
		http.Error(w, "Missing session_id or code in callback", http.StatusBadRequest)
		return
	}

	s.storeSession(sessionID)
	log.Printf("Authentication successful (%s)! Session ID: %s", flow, sessionID)
	http.Redirect(w, r, "/?success=true", http.StatusFound)
}

func (s *Server) exchangeCode(authURL, code string) (string, error) {
	exchangeURL := fmt.Sprintf("%s/api/code-exchange?code=%s", authURL, url.QueryEscape(code))
	log.Printf("Exchanging code at: %s", exchangeURL)

	resp, err := s.exchangeClient.Get(exchangeURL)
	if err != nil {
		return "", fmt.Errorf("code exchange request failed: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Success   bool   `json:"success"`
		SessionID string `json:"sessionId"`
		Error     string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode code exchange response: %w", err)
	}
	if !result.Success {
		return "", fmt.Errorf("code exchange rejected: %s", result.Error)
	}
	if result.SessionID == "" {
		return "", fmt.Errorf("code exchange returned empty sessionId")
	}

	return result.SessionID, nil
}

func (s *Server) storeSession(id string) {
	s.mu.Lock()
	s.sessionID = id
	s.mu.Unlock()
}

func (s *Server) getSession() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessionID
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	oldSessionID := s.sessionID
	s.sessionID = ""
	s.mu.Unlock()

	if oldSessionID != "" {
		log.Printf("Logged out (cleared session: %s)", oldSessionID)
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"session_id": s.getSession()})
}

func (s *Server) handleKeepAlive(w http.ResponseWriter, r *http.Request) {
	apiURL := r.URL.Query().Get("api_url")
	if apiURL == "" {
		http.Error(w, "Missing api_url parameter", http.StatusBadRequest)
		return
	}

	sessionID := s.getSession()
	if sessionID == "" {
		http.Error(w, "No active session", http.StatusUnauthorized)
		return
	}

	sessionURL := fmt.Sprintf("%s/user/session", apiURL)
	req, err := http.NewRequest("GET", sessionURL, nil)
	if err != nil {
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.Header.Set("Origin", s.baseURL)

	resp, err := s.exchangeClient.Do(req)
	if err != nil {
		log.Printf("Keep-alive request failed: %v", err)
		http.Error(w, "Keep-alive request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, io.LimitReader(resp.Body, 1<<20)) // 1 MiB cap

	if resp.StatusCode == http.StatusOK {
		log.Printf("Session keep-alive successful")
	} else {
		log.Printf("Session keep-alive returned status %d", resp.StatusCode)
	}
}
