package procrun

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// CalledProcessError is the error of a command that exited with a code other than 0, where the
// Python script used `check=True`.
type CalledProcessError struct {
	Code int
	Argv []string
}

func (e *CalledProcessError) Error() string {
	return fmt.Sprintf("command %v exited with code %d", e.Argv, e.Code)
}

// KeyError is the error of a missing field in a JSON reply.
type KeyError struct{ Key string }

func (e *KeyError) Error() string { return fmt.Sprintf("missing key %q", e.Key) }

// ConfigError is a configuration error; the message always names the file.
type ConfigError struct{ Msg string }

func (e *ConfigError) Error() string { return e.Msg }

// PyName is the name of the Python exception that err replaces. The scripts print it
// (`board: skipped (CalledProcessError)`), and the fixtures prove the output stays equal.
// A ConfigError also gives its message, on one line.
func PyName(err error) string {
	var (
		cfg     *ConfigError
		called  *CalledProcessError
		timeout *Timeout
		missing *NotFound
		key     *KeyError
		syntax  *json.SyntaxError
		typ     *json.UnmarshalTypeError
	)
	switch {
	case errors.As(err, &cfg):
		return "ConfigError: " + strings.Join(strings.Fields(cfg.Msg), " ")
	case errors.As(err, &called):
		return "CalledProcessError"
	case errors.As(err, &timeout):
		return "TimeoutExpired"
	case errors.As(err, &missing):
		return "FileNotFoundError"
	case errors.As(err, &key):
		return "KeyError"
	case errors.As(err, &syntax):
		return "JSONDecodeError"
	case errors.As(err, &typ):
		return "TypeError"
	}
	return fmt.Sprintf("%T", err)
}
