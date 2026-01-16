package models

type Request struct {
	Command string `json:"command"`
	Service string `json:"service,omitempty"`
	DryRun  bool   `json:"dry_run,omitempty"`
}

type Response struct {
	OK       bool     `json:"ok"`
	Message  string   `json:"message,omitempty"`
	Services []string `json:"services,omitempty"`
	Error    string   `json:"error,omitempty"`
	DryRun   bool     `json:"dry_run,omitempty"`
	Command  []string `json:"command,omitempty"`
}
