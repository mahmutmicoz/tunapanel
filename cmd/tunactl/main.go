package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"tunapanel/internal/config"
	"tunapanel/internal/models"
)

func main() {
	if os.Geteuid() == 0 {
		fmt.Fprintln(os.Stderr, "tunactl must not run as root")
		os.Exit(1)
	}

	dryRun := flag.Bool("dry-run", false, "show what would change without executing")
	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}

	var req models.Request
	req.DryRun = *dryRun

	switch args[0] {
	case "status":
		if len(args) != 1 {
			usage()
			os.Exit(2)
		}
		req.Command = "status"
	case "service":
		if len(args) < 2 {
			usage()
			os.Exit(2)
		}
		switch args[1] {
		case "list":
			if len(args) != 2 {
				usage()
				os.Exit(2)
			}
			req.Command = "service.list"
		case "start":
			if len(args) != 3 {
				usage()
				os.Exit(2)
			}
			req.Command = "service.start"
			req.Service = args[2]
		case "stop":
			if len(args) != 3 {
				usage()
				os.Exit(2)
			}
			req.Command = "service.stop"
			req.Service = args[2]
		default:
			usage()
			os.Exit(2)
		}
	default:
		usage()
		os.Exit(2)
	}

	resp, err := doRequest(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if !resp.OK {
		if resp.Error != "" {
			fmt.Fprintln(os.Stderr, "error:", resp.Error)
		} else {
			fmt.Fprintln(os.Stderr, "error: request failed")
		}
		os.Exit(1)
	}

	if resp.Message != "" {
		fmt.Println(resp.Message)
	}
	for _, svc := range resp.Services {
		fmt.Println(svc)
	}
}

func doRequest(req models.Request) (models.Response, error) {
	var out models.Response

	const requestTimeout = 5 * time.Second

	payload, err := json.Marshal(req)
	if err != nil {
		return out, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/v1/command", bytes.NewReader(payload))
	if err != nil {
		return out, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				dialer := net.Dialer{Timeout: requestTimeout}
				return dialer.DialContext(ctx, "unix", config.SocketPath)
			},
		},
		Timeout: requestTimeout,
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return out, fmt.Errorf("agent did not respond within %s", requestTimeout)
		}
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return out, fmt.Errorf("agent did not respond within %s", requestTimeout)
		}
		return out, fmt.Errorf("unable to reach agent socket %s: %w", config.SocketPath, err)
	}
	defer resp.Body.Close()

	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&out); err != nil {
		return out, err
	}

	if resp.StatusCode != http.StatusOK {
		if out.Error != "" {
			return out, errors.New(out.Error)
		}
		return out, fmt.Errorf("agent error: %s", resp.Status)
	}

	return out, nil
}

func usage() {
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  tunactl [--dry-run] status")
	fmt.Fprintln(os.Stderr, "  tunactl [--dry-run] service list")
	fmt.Fprintln(os.Stderr, "  tunactl [--dry-run] service start <name>")
	fmt.Fprintln(os.Stderr, "  tunactl [--dry-run] service stop <name>")
}
