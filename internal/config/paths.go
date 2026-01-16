package config

const (
	SocketPath      = "/run/tunapanel/agent.sock"
	LogPath         = "/var/log/tunapanel/agent.log"
	AuditLogPath    = "/var/log/tunapanel/audit.log"
	MaxRequestBytes = int64(64 * 1024)
	RateLimitPerSec = 5
)
