package webui

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf16"
)

//go:embed picker_windows.ps1
var pathPickerScript string

type pickPathRequest struct {
	Kind    string `json:"kind"`
	Initial string `json:"initial"`
}

func (s *Server) handlePickPath(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]bool{"available": runtime.GOOS == "windows"})
		return
	}
	if r.Method != http.MethodPost {
		apiError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	var req pickPathRequest
	if err := decodeBody(r, &req); err != nil {
		apiError(w, http.StatusBadRequest, "Invalid path picker request")
		return
	}
	switch req.Kind {
	case "open-bundle", "open-file", "save-bundle", "folder":
	default:
		apiError(w, http.StatusBadRequest, "Unknown picker kind")
		return
	}
	if len(req.Initial) > 4096 {
		apiError(w, http.StatusBadRequest, "Path is too long")
		return
	}
	if runtime.GOOS != "windows" {
		apiError(w, http.StatusNotImplemented, "Native file picker is only available on Windows")
		return
	}
	path, cancelled, err := pickWindowsPath(r.Context(), req.Kind, strings.TrimSpace(req.Initial))
	if err != nil {
		apiError(w, http.StatusInternalServerError, "Could not open file picker: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path, "cancelled": cancelled})
}

func pickWindowsPath(ctx context.Context, kind, initial string) (string, bool, error) {
	units := utf16.Encode([]rune(strings.TrimPrefix(pathPickerScript, "\ufeff")))
	data := make([]byte, len(units)*2)
	for i, unit := range units {
		binary.LittleEndian.PutUint16(data[i*2:], unit)
	}
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-STA", "-NonInteractive", "-EncodedCommand", base64.StdEncoding.EncodeToString(data))
	defaultDir := ""
	if executable, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(filepath.Dir(executable)), "CHATS")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			defaultDir = candidate
		}
	}
	cmd.Env = append(os.Environ(), "CCT_PICK_KIND="+kind, "CCT_PICK_INITIAL="+initial, "CCT_PICK_DEFAULT="+defaultDir)
	output, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 2 {
			return "", true, nil
		}
		if errors.As(err, &exit) && len(exit.Stderr) != 0 {
			return "", false, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(exit.Stderr)))
		}
		return "", false, err
	}
	path := strings.TrimSpace(strings.TrimPrefix(string(output), "\ufeff"))
	if path == "" {
		return "", false, errors.New("file picker returned an empty path")
	}
	return path, false, nil
}
