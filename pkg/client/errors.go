package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"unicode"
)

const (
	maxHTTPErrorBodyBytes    = 4 << 10
	maxHTTPErrorMessageRunes = 200
)

// HTTPError reports a non-2xx qBittorrent response. Message is a bounded,
// sanitized excerpt of the response body and may be empty.
type HTTPError struct {
	StatusCode int
	Message    string
}

// codedErrorWithCause preserves the public CodedError view while retaining
// the underlying error for callers that use errors.As. Keeping this as a
// wrapper avoids changing the layout of the exported CodedError type.
type codedErrorWithCause struct {
	coded *CodedError
	cause error
}

func (e *codedErrorWithCause) Error() string { return e.coded.Error() }
func (e *codedErrorWithCause) Unwrap() error { return e.cause }
func (e *codedErrorWithCause) As(target any) bool {
	destination, ok := target.(**CodedError)
	if !ok {
		return false
	}
	*destination = e.coded
	return true
}

func codedErrWrapf(code int, cause error, format string, args ...any) error {
	return &codedErrorWithCause{
		coded: &CodedError{Code: code, Message: fmt.Sprintf(format, args...)},
		cause: cause,
	}
}

func (e *HTTPError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("http error: %d", e.StatusCode)
	}
	return fmt.Sprintf("http error: %d (%s)", e.StatusCode, e.Message)
}

func isHTTPStatus(err error, status int) bool {
	var httpErr *HTTPError
	return errors.As(err, &httpErr) && httpErr.StatusCode == status
}

func newHTTPError(resp *http.Response, body []byte) *HTTPError {
	status := 0
	if resp != nil {
		status = resp.StatusCode
	}
	return &HTTPError{StatusCode: status, Message: truncateHTTPMessage(sanitizeHTTPBody(resp, body))}
}

func sanitizeHTTPBody(resp *http.Response, body []byte) string {
	if resp != nil {
		contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
		if mediaType, _, err := mime.ParseMediaType(contentType); err == nil {
			contentType = mediaType
		}
		if strings.EqualFold(contentType, "text/html") {
			return ""
		}
	}
	trimmed := strings.TrimSpace(string(body))
	if strings.HasPrefix(trimmed, "<") {
		return ""
	}
	return collapseServerWhitespace(SanitizeServerText(trimmed))
}

func truncateHTTPMessage(message string) string {
	runes := []rune(message)
	if len(runes) <= maxHTTPErrorMessageRunes {
		return message
	}
	return string(runes[:maxHTTPErrorMessageRunes]) + "…"
}

func sanitizeLoginHTTPBody(resp *http.Response, body []byte, password string) string {
	// The body may end mid-credential if it reached the read limit.
	if len(body) >= maxHTTPErrorBodyBytes {
		return ""
	}
	message := sanitizeHTTPBody(resp, body)
	if message == "" || password == "" {
		return truncateHTTPMessage(message)
	}

	// A login failure body is server-controlled and occasionally echoes a
	// submitted password in a JSON or URL-encoded diagnostic. Redact the
	// literal and common escaped forms without changing ordinary ban text.
	escaped, _ := json.Marshal(password)
	candidates := []string{
		string(escaped[1 : len(escaped)-1]),
		url.QueryEscape(password),
		url.PathEscape(password),
		collapseServerWhitespace(SanitizeServerText(password)),
		password,
	}
	replacements := make([]string, 0, len(candidates)*2)
	for _, candidate := range candidates {
		if candidate != "" {
			replacements = append(replacements, candidate, "[redacted]")
		}
	}
	return truncateHTTPMessage(strings.NewReplacer(replacements...).Replace(message))
}

// SanitizeServerText makes text safe for terminal and structured CLI output.
// It preserves ordinary whitespace so callers displaying server-provided
// fields do not unexpectedly change their formatting.
func SanitizeServerText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
}

func collapseServerWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
