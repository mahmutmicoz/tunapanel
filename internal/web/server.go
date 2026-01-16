package web

import (
	"embed"
	"html/template"
	"net/http"
	"time"
)

const (
	defaultAddr  = "127.0.0.1:8080"
	agentTimeout = 800 * time.Millisecond
)

//go:embed templates/*.html
var templateFS embed.FS

type Server struct {
	Addr    string
	Handler http.Handler
}

func NewServer(client *AgentClient) (*Server, error) {
	tmpl, err := template.ParseFS(templateFS, "templates/status.html")
	if err != nil {
		return nil, err
	}

	handlers := &Handlers{
		client: client,
		tmpl:   tmpl,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", handlers.Index)
	mux.HandleFunc("/health", handlers.Health)
	mux.HandleFunc("/status", handlers.Status)
	mux.HandleFunc("/services", handlers.Services)

	return &Server{
		Addr:    defaultAddr,
		Handler: mux,
	}, nil
}

func DefaultAgentClient(socketPath string) *AgentClient {
	return NewAgentClient(socketPath, agentTimeout)
}
