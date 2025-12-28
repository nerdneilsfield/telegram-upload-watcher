package ziputil

import (
	"errors"
	"io"
	"strings"

	zip "github.com/yeka/zip"
)

func ReadFile(file *zip.File, passwords []string) ([]byte, error) {
	data, err := readOnce(file)
	if err == nil {
		return data, nil
	}
	if len(passwords) == 0 {
		return nil, err
	}

	lastErr := err
	for _, password := range passwords {
		password = strings.TrimSpace(password)
		if password == "" {
			continue
		}
		file.SetPassword(password)
		data, err := readOnce(file)
		if err == nil {
			return data, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("zip passwords exhausted")
	}
	return nil, lastErr
}

func readOnce(file *zip.File) ([]byte, error) {
	handle, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	return io.ReadAll(handle)
}
