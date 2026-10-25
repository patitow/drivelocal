package main

import (
	"net/http"
	"strings"
	"unicode/utf8"
)

// Validation errors
var (
	ErrUsernameTooShort = "Username must be at least 3 characters"
	ErrUsernameTooLong  = "Username must be at most 50 characters"
	ErrPasswordTooShort = "Password must be at least 6 characters"
	ErrPasswordTooLong  = "Password must be at most 128 characters"
	ErrEmailInvalid     = "Invalid email format"
	ErrPathInvalid      = "Invalid path format"
)

// validateUsername validates username
func validateUsername(username string) error {
	username = strings.TrimSpace(username)
	if len(username) < 3 {
		return &ValidationError{Message: ErrUsernameTooShort}
	}
	if len(username) > 50 {
		return &ValidationError{Message: ErrUsernameTooLong}
	}
	// Check for valid characters (alphanumeric, underscore, hyphen)
	for _, r := range username {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			return &ValidationError{Message: "Username contains invalid characters"}
		}
	}
	return nil
}

// validatePassword validates password
func validatePassword(password string) error {
	if len(password) < 6 {
		return &ValidationError{Message: ErrPasswordTooShort}
	}
	if len(password) > 128 {
		return &ValidationError{Message: ErrPasswordTooLong}
	}
	return nil
}

// validateEmail validates email (basic validation)
func validateEmail(email string) error {
	email = strings.TrimSpace(email)
	if len(email) < 5 || len(email) > 254 {
		return &ValidationError{Message: ErrEmailInvalid}
	}
	if !strings.Contains(email, "@") || !strings.Contains(email, ".") {
		return &ValidationError{Message: ErrEmailInvalid}
	}
	// Basic email format check
	parts := strings.Split(email, "@")
	if len(parts) != 2 || len(parts[0]) == 0 || len(parts[1]) == 0 {
		return &ValidationError{Message: ErrEmailInvalid}
	}
	return nil
}

// validatePath validates file path
func validatePath(path string) error {
	// Check for null bytes or invalid UTF-8
	if !utf8.ValidString(path) {
		return &ValidationError{Message: ErrPathInvalid}
	}
	// Check for path traversal attempts (basic check, main validation is in handlers).
	// Só o segmento ".." inteiro: "Espera... Volta" é nome de pasta válido.
	if strings.Contains(path, "\x00") {
		return &ValidationError{Message: ErrPathInvalid}
	}
	for _, seg := range strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return &ValidationError{Message: ErrPathInvalid}
		}
	}
	return nil
}

// ValidationError represents a validation error
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// handleValidationError writes validation error response
func handleValidationError(w http.ResponseWriter, err error) {
	if validationErr, ok := err.(*ValidationError); ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"` + validationErr.Message + `"}`))
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"Validation failed"}`))
	}
}

