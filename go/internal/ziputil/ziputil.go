package ziputil

import (
	"errors"
	"fmt"
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
