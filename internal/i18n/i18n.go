package i18n

import (
	"fmt"
	"slices"
	"strings"
)

type Language string

const (
	Chinese Language = "zh"
	English Language = "en"
)

func (l Language) Valid() bool { return l == Chinese || l == English }

// Text translates application messages, never user content or rendered units.
func (l Language) Text(message string) string {
	if l == English {
		if translated, ok := english[message]; ok {
			return translated
		}
	}
	return message
}

// Errorf retains the message template and arguments for translation at the UI.
// The wrapped error keeps fmt.Errorf's normal errors.Is/As behavior.
func Errorf(format string, args ...any) error {
	return &messageError{format: format, args: args, err: fmt.Errorf(format, args...)}
}

type messageError struct {
	format string
	args   []any
	err    error
}

func (e *messageError) Error() string { return e.err.Error() }
func (e *messageError) Unwrap() error { return e.err }

func (l Language) Error(err error) string {
	if err == nil {
		return ""
	}
	if message, ok := err.(*messageError); ok {
		args := slices.Clone(message.args)
		for i, arg := range args {
			if nested, ok := arg.(error); ok {
				args[i] = l.Error(nested)
			}
		}
		return fmt.Sprintf(strings.ReplaceAll(l.Text(message.format), "%w", "%v"), args...)
	}
	return err.Error()
}
