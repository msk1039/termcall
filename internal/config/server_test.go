package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseServerRejectsMissingSecret(t *testing.T) {
	environment := map[string]string{
		"TERMCALL_PUBLIC_IP": "203.0.113.10",
	}
	_, err := ParseServer(nil, mapLookup(environment))
	if err == nil {
		t.Fatal("expected a missing-secret error")
	}
}

func TestParseServerRejectsWrappedPorts(t *testing.T) {
	environment := validEnvironment()
	environment["TERMCALL_RELAY_PORT_MIN"] = "70000"

	_, err := ParseServer(nil, mapLookup(environment))
	if err == nil {
		t.Fatal("expected an invalid-port error")
	}
}

func TestParseServerUsesValidatedValues(t *testing.T) {
	environment := validEnvironment()
	environment["TERMCALL_ALLOWED_ORIGINS"] = "https://example.com, https://admin.example.com"

	config, err := ParseServer([]string{"-max-room-size", "4"}, mapLookup(environment))
	if err != nil {
		t.Fatalf("ParseServer: %v", err)
	}
	if config.HTTPAddress != "127.0.0.1:8080" {
		t.Fatalf("HTTPAddress = %q", config.HTTPAddress)
	}
	if config.MaxRoomSize != 4 {
		t.Fatalf("MaxRoomSize = %d", config.MaxRoomSize)
	}
	if len(config.AllowedOrigins) != 2 {
		t.Fatalf("AllowedOrigins = %#v", config.AllowedOrigins)
	}
}

func TestLoadServerReadsEnvFileWithoutOverridingProcessEnvironment(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, ".env")
	contents := strings.Join([]string{
		"TERMCALL_PUBLIC_IP=1.1.1.1",
		"TERMCALL_TURN_SECRET=" + testTURNSecret,
		"TERMCALL_WS_PORT=9090",
	}, "\n")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	t.Setenv("TERMCALL_PUBLIC_IP", "1.1.1.1")
	t.Setenv("TERMCALL_TURN_SECRET", testTURNSecret)
	t.Setenv("TERMCALL_WS_PORT", "8081")

	config, err := LoadServer([]string{"-max-room-size", "3"}, path)
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if config.HTTPAddress != "127.0.0.1:8081" || config.MaxRoomSize != 3 {
		t.Fatalf("loaded config = %#v", config)
	}

	values, err := readEnvFile(filepath.Join(directory, "missing"))
	if err != nil || len(values) != 0 {
		t.Fatalf("missing env file = %#v, %v", values, err)
	}
}

func TestParseServerRejectsInvalidTypedEnvironment(t *testing.T) {
	tests := []struct {
		key   string
		value string
	}{
		{key: "TERMCALL_WS_PORT", value: "not-a-port"},
		{key: "TERMCALL_MESSAGE_RATE", value: "fast"},
		{key: "TERMCALL_TURN_ALLOW_PRIVATE_PEERS", value: "sometimes"},
		{key: "TERMCALL_TURN_CREDENTIAL_TTL", value: "later"},
	}
	for _, test := range tests {
		t.Run(test.key, func(t *testing.T) {
			environment := validEnvironment()
			environment[test.key] = test.value
			if _, err := ParseServer(nil, mapLookup(environment)); err == nil {
				t.Fatal("invalid typed value unexpectedly succeeded")
			}
		})
	}
}

func TestParseServerRejectsInvalidRangesAndArguments(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
		args  []string
	}{
		{name: "bind", key: "TERMCALL_WS_BIND", value: "localhost"},
		{name: "room size", key: "TERMCALL_MAX_ROOM_SIZE", value: "1"},
		{name: "old secret", key: "TERMCALL_TURN_SECRET", value: "termcall"},
		{name: "low-diversity secret", key: "TERMCALL_TURN_SECRET", value: strings.Repeat("s", minimumTURNSecretBytes)},
		{name: "TTL", key: "TERMCALL_TURN_CREDENTIAL_TTL", value: "2h"},
		{name: "per-user quota", key: "TERMCALL_TURN_PER_USER_QUOTA", value: "11"},
		{name: "total quota", key: "TERMCALL_TURN_TOTAL_QUOTA", value: "1"},
		{name: "connection limit", key: "TERMCALL_MAX_CONNECTIONS_PER_IP", value: "0"},
		{name: "total connection limit", key: "TERMCALL_MAX_CONNECTIONS", value: "10"},
		{name: "room limit", key: "TERMCALL_MAX_ROOMS", value: "1001", args: []string{"-max-connections", "1000"}},
		{name: "message rate", key: "TERMCALL_MESSAGE_RATE", value: "0.5"},
		{name: "message burst", key: "TERMCALL_MESSAGE_BURST", value: "0"},
		{name: "relay order", key: "TERMCALL_RELAY_PORT_MIN", value: "60000", args: []string{"-relay-max", "50000"}},
		{name: "extra argument", args: []string{"unexpected"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			environment := validEnvironment()
			if test.key != "" {
				environment[test.key] = test.value
			}
			if _, err := ParseServer(test.args, mapLookup(environment)); err == nil {
				t.Fatal("invalid configuration unexpectedly succeeded")
			}
		})
	}
}

func TestParseServerRejectsSpecialUsePublicIPAndUnsafeOrigin(t *testing.T) {
	environment := validEnvironment()
	environment["TERMCALL_PUBLIC_IP"] = "203.0.113.10"
	if _, err := ParseServer(nil, mapLookup(environment)); err == nil {
		t.Fatal("documentation IP unexpectedly accepted as public")
	}

	environment = validEnvironment()
	environment["TERMCALL_ALLOWED_ORIGINS"] = "*"
	if _, err := ParseServer(nil, mapLookup(environment)); err == nil {
		t.Fatal("wildcard origin unexpectedly accepted")
	}
}

func TestParseEnvTreatsValuesAsData(t *testing.T) {
	values, err := parseEnv(strings.NewReader("TERMCALL_WS_PORT=8080\nDANGEROUS=$(touch /tmp/nope)\nSINGLE='multi word value'\n"))
	if err != nil {
		t.Fatalf("parseEnv: %v", err)
	}
	if values["DANGEROUS"] != "$(touch /tmp/nope)" {
		t.Fatalf("value was interpreted: %q", values["DANGEROUS"])
	}
	if values["SINGLE"] != "multi word value" {
		t.Fatalf("single-quoted value = %q", values["SINGLE"])
	}
}

func TestParseEnvRejectsMalformedLinesAndQuotes(t *testing.T) {
	tests := []string{
		"NO_EQUALS",
		"BAD-KEY=value",
		"VALUE=\"missing",
		"VALUE='bad'quote'",
	}
	for _, contents := range tests {
		if _, err := parseEnv(strings.NewReader(contents)); err == nil {
			t.Errorf("parseEnv(%q) unexpectedly succeeded", contents)
		}
	}
}

func validEnvironment() map[string]string {
	return map[string]string{
		"TERMCALL_PUBLIC_IP":   "1.1.1.1",
		"TERMCALL_TURN_SECRET": testTURNSecret,
	}
}

var testTURNSecret = strings.Repeat("test-only-01234567-", 2)

func mapLookup(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}
