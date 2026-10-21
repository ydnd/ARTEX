package config

import (
	"strings"
	"testing"
)

func TestRedactNeverIncludesConnectionSecrets(t *testing.T) {
	const secret = "private-database-sentinel"
	for _, dsn := range []string{
		"postgres://operator:" + secret + "@localhost/example?sslmode=disable",
		"host=localhost user=operator password='" + secret + "' dbname=example",
		"postgres://localhost/example?password=" + secret,
		"host=localhost password='" + secret,
		"postgres://operator:" + secret + "%broken@localhost/example",
	} {
		got := Redact(dsn)
		if strings.Contains(got, secret) {
			t.Fatal("redacted database log exposed a secret")
		}
		if got == "" {
			t.Fatal("expected a safe database description")
		}
	}
}
