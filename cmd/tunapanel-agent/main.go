package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"tunapanel/internal/config"
	"tunapanel/internal/logger"
	"tunapanel/internal/models"
	"tunapanel/internal/services"
)

func main() {
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "tunapanel-agent must run as root")
		os.Exit(1)
	}

	log := logger.New(config.LogPath)
	log.Printf("starting tunapanel-agent")
	auditLog := logger.New(config.AuditLogPath)
	auditLog.SetPrefix("tunapanel-audit ")
	limiter := newRateLimiter(config.RateLimitPerSec)

	socketDir := filepath.Dir(config.SocketPath)
	if err := os.MkdirAll(socketDir, 0750); err != nil {
		log.Printf("failed to create socket directory: %v", err)
		os.Exit(1)
	}
	if err := os.RemoveAll(config.SocketPath); err != nil {
		log.Printf("failed to remove existing socket: %v", err)
		os.Exit(1)
	}

	listener, err := net.Listen("unix", config.SocketPath)
	if err != nil {
		log.Printf("failed to listen on socket: %v", err)
		os.Exit(1)
	}
	defer os.Remove(config.SocketPath)

	socketMode := os.FileMode(0600)
	if grp, err := user.LookupGroup("tunapanel"); err == nil {
		if gid, err := strconv.Atoi(grp.Gid); err == nil {
			if err := os.Chown(config.SocketPath, 0, gid); err != nil {
				log.Printf("warning: failed to chown socket: %v", err)
			} else {
				socketMode = 0660
			}
		}
	} else {
		log.Printf("warning: group 'tunapanel' not found; socket restricted to root")
	}
	if err := os.Chmod(config.SocketPath, socketMode); err != nil {
		log.Printf("warning: failed to chmod socket: %v", err)
	}

	server := &http.Server{
		Handler:           handler(log, auditLog, limiter),
		ConnContext:       connContext,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(listener)
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Printf("signal received: %s", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("shutdown error: %v", err)
		}
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("server error: %v", err)
		}
	}
}

func handler(log *log.Logger, audit *log.Logger, limiter *rateLimiter) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/command", func(w http.ResponseWriter, r *http.Request) {
		reqID := newRequestID()
		w.Header().Set("X-Request-Id", reqID)
		peer := peerFromContext(r.Context())

		if limiter != nil && !limiter.Allow(peer.UID) {
			resp := models.Response{
				OK:    false,
				Error: "rate limit exceeded",
			}
			writeJSON(w, http.StatusTooManyRequests, resp)
			recordRequest(log, audit, reqID, peer, "", "", false, resp.OK, resp.Error)
			return
		}

		if r.Method != http.MethodPost {
			resp := models.Response{
				OK:    false,
				Error: "method not allowed",
			}
			writeJSON(w, http.StatusMethodNotAllowed, resp)
			recordRequest(log, audit, reqID, peer, "", "", false, resp.OK, resp.Error)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, config.MaxRequestBytes)
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()

		var req models.Request
		if err := dec.Decode(&req); err != nil {
			resp := models.Response{
				OK:    false,
				Error: "invalid JSON request",
			}
			writeJSON(w, http.StatusBadRequest, resp)
			recordRequest(log, audit, reqID, peer, "", "", false, resp.OK, resp.Error)
			return
		}
		if err := ensureEOF(dec); err != nil {
			resp := models.Response{
				OK:    false,
				Error: "invalid JSON request",
			}
			writeJSON(w, http.StatusBadRequest, resp)
			recordRequest(log, audit, reqID, peer, "", "", req.DryRun, resp.OK, resp.Error)
			return
		}

		resp, status := handleCommand(req)
		writeJSON(w, status, resp)
		recordRequest(log, audit, reqID, peer, req.Command, req.Service, req.DryRun, resp.OK, resp.Error)
	})

	return mux
}

func handleCommand(req models.Request) (models.Response, int) {
	resp := models.Response{OK: true, DryRun: req.DryRun}

	switch req.Command {
	case "status":
		resp.Message = "ok"
		return resp, http.StatusOK
	case "service.list":
		servicesList, message, err := services.ListEnabledServices(req.DryRun)
		if err != nil {
			return errorResponse(err, req.DryRun)
		}
		resp.Services = servicesList
		resp.Message = message
		return resp, http.StatusOK
	case "service.start":
		if req.Service == "" {
			return badRequest("service name is required", req.DryRun)
		}
		name, err := services.NormalizeServiceName(req.Service)
		if err != nil {
			return badRequest(err.Error(), req.DryRun)
		}
		cmd, message, err := services.StartService(name, req.DryRun)
		if err != nil {
			return errorResponse(err, req.DryRun)
		}
		resp.Command = cmd
		resp.Message = message
		return resp, http.StatusOK
	case "service.stop":
		if req.Service == "" {
			return badRequest("service name is required", req.DryRun)
		}
		name, err := services.NormalizeServiceName(req.Service)
		if err != nil {
			return badRequest(err.Error(), req.DryRun)
		}
		cmd, message, err := services.StopService(name, req.DryRun)
		if err != nil {
			return errorResponse(err, req.DryRun)
		}
		resp.Command = cmd
		resp.Message = message
		return resp, http.StatusOK
	default:
		return badRequest("unknown command", req.DryRun)
	}
}

func writeJSON(w http.ResponseWriter, status int, resp models.Response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	_ = enc.Encode(resp)
}

func ensureEOF(dec *json.Decoder) error {
	if dec.More() {
		return errors.New("extra data in request")
	}
	var extra interface{}
	if err := dec.Decode(&extra); err == io.EOF {
		return nil
	}
	return errors.New("extra data in request")
}

func badRequest(message string, dryRun bool) (models.Response, int) {
	return models.Response{
		OK:     false,
		Error:  message,
		DryRun: dryRun,
	}, http.StatusBadRequest
}

func errorResponse(err error, dryRun bool) (models.Response, int) {
	return models.Response{
		OK:     false,
		Error:  err.Error(),
		DryRun: dryRun,
	}, http.StatusInternalServerError
}

type peerInfo struct {
	UID int
	GID int
	PID int
}

type peerKeyType int

const peerKey peerKeyType = 0

func connContext(ctx context.Context, c net.Conn) context.Context {
	peer, err := peerFromConn(c)
	if err != nil {
		return ctx
	}
	return context.WithValue(ctx, peerKey, peer)
}

func peerFromContext(ctx context.Context) peerInfo {
	peer, ok := ctx.Value(peerKey).(peerInfo)
	if !ok {
		return peerInfo{UID: -1, GID: -1, PID: -1}
	}
	return peer
}

func peerFromConn(c net.Conn) (peerInfo, error) {
	unixConn, ok := c.(*net.UnixConn)
	if !ok {
		return peerInfo{}, errors.New("unsupported connection type")
	}
	ucred, err := getUcred(unixConn)
	if err != nil {
		return peerInfo{}, err
	}
	return peerInfo{UID: int(ucred.Uid), GID: int(ucred.Gid), PID: int(ucred.Pid)}, nil
}

func getUcred(conn *net.UnixConn) (*syscall.Ucred, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return nil, err
	}
	var ucred *syscall.Ucred
	var ctrlErr error
	if err := raw.Control(func(fd uintptr) {
		ucred, ctrlErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return nil, err
	}
	if ctrlErr != nil {
		return nil, ctrlErr
	}
	return ucred, nil
}

type rateLimiter struct {
	mu        sync.Mutex
	perSecond int
	buckets   map[int]*rateBucket
}

type rateBucket struct {
	second int64
	count  int
}

func newRateLimiter(perSecond int) *rateLimiter {
	if perSecond <= 0 {
		return nil
	}
	return &rateLimiter{
		perSecond: perSecond,
		buckets:   make(map[int]*rateBucket),
	}
}

func (r *rateLimiter) Allow(uid int) bool {
	now := time.Now().Unix()

	r.mu.Lock()
	defer r.mu.Unlock()

	bucket, ok := r.buckets[uid]
	if !ok || bucket.second != now {
		r.buckets[uid] = &rateBucket{second: now, count: 1}
		if len(r.buckets) > 1024 {
			r.prune(now)
		}
		return true
	}

	if bucket.count >= r.perSecond {
		return false
	}

	bucket.count++
	return true
}

func (r *rateLimiter) prune(now int64) {
	for uid, bucket := range r.buckets {
		if bucket.second != now {
			delete(r.buckets, uid)
		}
	}
}

func newRequestID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}

func recordRequest(log *log.Logger, audit *log.Logger, reqID string, peer peerInfo, command string, service string, dryRun bool, ok bool, errMsg string) {
	log.Printf("req_id=%s uid=%d gid=%d pid=%d command=%s service=%s dry_run=%t ok=%t error=%s",
		reqID, peer.UID, peer.GID, peer.PID, command, service, dryRun, ok, errMsg)
	if audit != nil {
		audit.Printf("req_id=%s uid=%d gid=%d pid=%d command=%s service=%s dry_run=%t ok=%t error=%s",
			reqID, peer.UID, peer.GID, peer.PID, command, service, dryRun, ok, errMsg)
	}
}
