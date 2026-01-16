package logger

import (
	"log"
	"os"
	"path/filepath"
)

func New(path string) *log.Logger {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return log.New(os.Stderr, "tunapanel-agent ", log.LstdFlags)
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
	if err != nil {
		return log.New(os.Stderr, "tunapanel-agent ", log.LstdFlags)
	}

	return log.New(file, "tunapanel-agent ", log.LstdFlags)
}
