package services

import (
	"bufio"
	"errors"
	"fmt"
	"strings"

	"tunapanel/internal/executor"
)

const maxServiceNameLen = 128

func NormalizeServiceName(input string) (string, error) {
	name := strings.TrimSpace(input)
	if name == "" {
		return "", errors.New("service name is required")
	}
	if strings.HasPrefix(name, "-") {
		return "", errors.New("invalid service name")
	}
	if len(name) > maxServiceNameLen {
		return "", errors.New("service name is too long")
	}
	if strings.Contains(name, "/") {
		return "", errors.New("invalid service name")
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case strings.ContainsRune("@._:-", r):
		default:
			return "", errors.New("invalid service name")
		}
	}

	if !strings.HasSuffix(name, ".service") {
		name += ".service"
	}

	return name, nil
}

func ListEnabledServices(dryRun bool) ([]string, string, error) {
	cmd := []string{
		"systemctl",
		"list-unit-files",
		"--type=service",
		"--state=enabled",
		"--no-legend",
		"--no-pager",
	}

	message := ""
	if dryRun {
		message = "dry-run has no effect on service.list"
	}

	output, err := executor.Run(cmd)
	if err != nil {
		return nil, message, err
	}

	var services []string
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		services = append(services, fields[0])
	}
	if err := scanner.Err(); err != nil {
		return nil, message, err
	}

	return services, message, nil
}

func StartService(name string, dryRun bool) ([]string, string, error) {
	cmd := []string{"systemctl", "start", name}
	if dryRun {
		return cmd, fmt.Sprintf("dry-run: would run %s", strings.Join(cmd, " ")), nil
	}

	_, err := executor.Run(cmd)
	if err != nil {
		return cmd, "", err
	}

	return cmd, fmt.Sprintf("service started: %s", name), nil
}

func StopService(name string, dryRun bool) ([]string, string, error) {
	cmd := []string{"systemctl", "stop", name}
	if dryRun {
		return cmd, fmt.Sprintf("dry-run: would run %s", strings.Join(cmd, " ")), nil
	}

	_, err := executor.Run(cmd)
	if err != nil {
		return cmd, "", err
	}

	return cmd, fmt.Sprintf("service stopped: %s", name), nil
}
