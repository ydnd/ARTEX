package db

import (
	"strings"
	"testing"
)

func TestOpenDoesNotExposeMalformedDSNCredentials(t *testing.T) {
	const sentinel = "private-database-sentinel"
	_, err := Open("host=localhost password='" + sentinel)
	if err == nil {
		t.Fatal("expected invalid connection configuration to fail")
	}
	if strings.Contains(err.Error(), sentinel) {
		t.Fatal("database error exposed submitted credentials")
	}
}
