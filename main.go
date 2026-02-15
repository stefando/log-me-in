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
	"sync"
)

//go:embed static/*
var staticFiles embed.FS

type Server struct {
	port       int
	defaultURL string
	sessionID  string
	mu         sync.RWMutex
}

func main() {
	port := flag.Int("port", 8080, "Port to run the server on")
	env := flag.String("env", "gummi-dos", "Environment name")
	tenant := flag.String("tenant", "arasaka", "Tenant code")
	flag.Parse()

	defaultURL := fmt.Sprintf("https://api.%s.%s.saas.cmddev.thermofisher.com", *tenant, *env)
	s := &Server{port: *port, defaultURL: defaultURL}

	http.HandleFunc("/", s.handleIndex)
	http.HandleFunc("/config", s.handleConfig)
	http.HandleFunc("/login", s.handleLogin)
	http.HandleFunc("/logout", s.handleLogout)
	http.HandleFunc("/callback", s.handleCallback)
	http.HandleFunc("/session", s.handleGetSession)
	http.HandleFunc("/keep-alive", s.handleKeepAlive)

	addr := fmt.Sprintf(":%d", s.port)
	log.Printf("🔐 Auth server running at http://localhost%s\n", addr)
	log.Printf("Open this URL in your browser to authenticate\n")
	log.Fatal(http.ListenAndServe(addr, nil))
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

	// Build OAuth login URL
	redirectURI := fmt.Sprintf("http://localhost:%d/callback", s.port)
	loginURL := fmt.Sprintf("%s/user/login?redirect_uri=%s",
		apiURL,
		url.QueryEscape(redirectURI))

	log.Printf("Redirecting to: %s\n", loginURL)
	http.Redirect(w, r, loginURL, http.StatusFound)
}

func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		log.Printf("ERROR: Missing session_id in callback")
		http.Error(w, "Missing session_id in callback", http.StatusBadRequest)
		return
	}

	// Store session
	s.mu.Lock()
	s.sessionID = sessionID
	s.mu.Unlock()

	log.Printf("✅ Authentication successful! Session ID: %s\n", sessionID)

	// Redirect back to index with success
	http.Redirect(w, r, "/?success=true", http.StatusFound)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	oldSessionID := s.sessionID
	s.sessionID = ""
	s.mu.Unlock()

	if oldSessionID != "" {
		log.Printf("🔓 Logged out (cleared session: %s)\n", oldSessionID)
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	sessionID := s.sessionID
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	response := map[string]string{
		"session_id": sessionID,
	}
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleKeepAlive(w http.ResponseWriter, r *http.Request) {
	apiURL := r.URL.Query().Get("api_url")
	if apiURL == "" {
		http.Error(w, "Missing api_url parameter", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	sessionID := s.sessionID
	s.mu.RUnlock()

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
	req.Header.Set("Origin", fmt.Sprintf("http://localhost:%d", s.port))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("Keep-alive request failed: %v", err)
		http.Error(w, "Keep-alive request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)

	if resp.StatusCode == http.StatusOK {
		log.Printf("♻️ Session keep-alive successful")
	} else {
		log.Printf("⚠️ Session keep-alive returned status %d", resp.StatusCode)
	}
}
