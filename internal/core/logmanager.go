package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type LogManager struct {
	tempDir  string
	logFiles map[string]string
	mu       sync.Mutex
}

// purgeStaleLogDirs removes gb-logs-* temp dirs from previous runs older than
// seven days. Best-effort: current-run logs are kept so the printed path stays
// reviewable after gb exits.
func purgeStaleLogDirs() {
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "gb-logs-") {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.RemoveAll(filepath.Join(os.TempDir(), e.Name()))
	}
}

func NewLogManager() (*LogManager, error) {
	purgeStaleLogDirs()

	tempDir, err := os.MkdirTemp("", "gb-logs-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	return &LogManager{
		tempDir:  tempDir,
		logFiles: make(map[string]string),
	}, nil
}

func (lm *LogManager) CreateLogFile(relPath string) (*os.File, error) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	sanitized := strings.ReplaceAll(relPath, "/", "_")
	sanitized = strings.ReplaceAll(sanitized, "\\", "_")

	logPath := filepath.Join(lm.tempDir, fmt.Sprintf("%s.log", sanitized))

	f, err := os.Create(logPath)
	if err != nil {
		return nil, err
	}

	lm.logFiles[relPath] = logPath
	return f, nil
}

func (lm *LogManager) GetLogPath(relPath string) (string, bool) {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	path, ok := lm.logFiles[relPath]
	return path, ok
}

func (lm *LogManager) GetAllLogPaths() map[string]string {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	result := make(map[string]string, len(lm.logFiles))
	for k, v := range lm.logFiles {
		result[k] = v
	}
	return result
}

func (lm *LogManager) ReadLog(relPath string) (string, error) {
	logPath, ok := lm.GetLogPath(relPath)
	if !ok {
		return "", fmt.Errorf("no log file for %s", relPath)
	}

	content, err := os.ReadFile(logPath)
	if err != nil {
		return "", err
	}

	return string(content), nil
}

func (lm *LogManager) Cleanup() error {
	return os.RemoveAll(lm.tempDir)
}

func (lm *LogManager) GetTempDir() string {
	return lm.tempDir
}
