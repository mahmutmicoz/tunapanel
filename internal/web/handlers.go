package web

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"time"
)

type Handlers struct {
	client *AgentClient
	tmpl   *template.Template
}

type statusPayload struct {
	OK           bool     `json:"ok"`
	AgentOK      bool     `json:"agent_ok"`
	AgentMessage string   `json:"agent_message,omitempty"`
	AgentError   string   `json:"agent_error,omitempty"`
	Error        string   `json:"error,omitempty"`
	Services     []string `json:"services,omitempty"`
}

type statusPage struct {
	WebOK         bool
	AgentOK       bool
	AgentMessage  string
	AgentError    string
	Services      []string
	ServiceState  string
	TotalServices int
	CheckedAt     string
}

func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeHealth(w, http.StatusMethodNotAllowed, false)
		return
	}

	writeHealth(w, http.StatusOK, true)
}

func (h *Handlers) Status(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, statusPayload{OK: false})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.client.timeout)
	defer cancel()

	resp, err := h.client.Status(ctx)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, statusPayload{
			OK:         false,
			AgentOK:    false,
			AgentError: err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, statusPayload{
		OK:           true,
		AgentOK:      true,
		AgentMessage: resp.Message,
	})
}

func (h *Handlers) Services(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, statusPayload{OK: false})
		return
	}

	state := r.URL.Query().Get("state")
	if state == "" {
		state = "enabled"
	}
	if state != "enabled" && state != "running" {
		writeJSON(w, http.StatusBadRequest, statusPayload{
			OK:    false,
			Error: "invalid service state",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.client.timeout)
	defer cancel()

	resp, err := h.client.ListServices(ctx, state)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, statusPayload{
			OK:         false,
			AgentOK:    false,
			AgentError: err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, statusPayload{
		OK:       true,
		AgentOK:  true,
		Services: resp.Services,
	})
}

func (h *Handlers) Index(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.client.timeout)
	defer cancel()

	page := statusPage{
		WebOK:        true,
		ServiceState: "enabled",
		CheckedAt:    time.Now().Format(time.RFC3339),
	}

	resp, err := h.client.Status(ctx)
	if err != nil {
		page.AgentOK = false
		page.AgentError = err.Error()
		renderTemplate(w, h.tmpl, page)
		return
	}

	page.AgentOK = true
	page.AgentMessage = resp.Message

	servicesResp, err := h.client.ListServices(ctx, page.ServiceState)
	if err == nil {
		page.Services = servicesResp.Services
		page.TotalServices = len(page.Services)
	}

	renderTemplate(w, h.tmpl, page)
}

func writeJSON(w http.ResponseWriter, status int, payload statusPayload) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	_ = enc.Encode(payload)
}

func writeHealth(w http.ResponseWriter, status int, ok bool) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	_ = enc.Encode(struct {
		OK bool `json:"ok"`
	}{OK: ok})
}

func renderTemplate(w http.ResponseWriter, tmpl *template.Template, data statusPage) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}
