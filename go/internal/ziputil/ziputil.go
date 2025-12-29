package ziputil

import (
	"errors"
	"fmt"
	"io"
	"strings"

	zip "github.com/yeka/zip"
)

func ReadFile(file *zip.File, passwords []string) ([]byte, error) {
	if file == nil {
		return nil, errors.New("zip file is nil")
	}
	if !file.IsEncrypted() {
		return readOnce(file)
	}
	if len(passwords) == 0 {
		return nil, errors.New("zip entry is encrypted but no passwords provided")
	}

	var lastErr error
	var attemptErrors []string
	attempts := 0
	for _, password := range passwords {
		password = strings.TrimSpace(password)
		if password == "" {
			continue
		}
		attempts++
		file.SetPassword(password)
		data, err := readOnce(file)
		if err == nil {
			return data, nil
		}
		lastErr = err
		attemptErrors = append(attemptErrors, classifyErr(err))
	}
	if lastErr == nil {
		lastErr = errors.New("zip passwords exhausted")
	}
	if len(attemptErrors) > 0 {
		return nil, fmt.Errorf(
			"zip password check failed after %d attempt(s): %s",
			attempts,
			strings.Join(attemptErrors, "; "),
		)
	}
	return nil, fmt.Errorf("zip password check failed after %d attempt(s): %w", attempts, lastErr)
}

func readOnce(file *zip.File) (data []byte, err error) {
	if file == nil {
		return nil, errors.New("zip file is nil")
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("zip open panic: %v", r)
		}
	}()
	handle, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	return io.ReadAll(handle)
}

func classifyErr(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, zip.ErrPassword):
		return "invalid password"
	case errors.Is(err, zip.ErrChecksum):
		return "checksum error (likely wrong password)"
	case errors.Is(err, zip.ErrAlgorithm):
		return "unsupported compression algorithm"
	default:
		return err.Error()
	}
}
