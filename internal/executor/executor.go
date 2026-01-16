package executor

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

func Run(args []string) (string, error) {
	if len(args) == 0 {
		return "", errors.New("empty command")
	}

	cmd := exec.Command(args[0], args[1:]...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return "", fmt.Errorf("%w: %s", err, msg)
		}
		return "", err
	}

	return stdout.String(), nil
}
