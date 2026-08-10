package turn

import (
	"bytes"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	pionturn "github.com/pion/turn/v4"
)

func TestCredentialIssuerCreatesExpiringRESTCredential(t *testing.T) {
	secret := strings.Repeat("s", 32)
	issuer := NewCredentialIssuer(secret, 10*time.Minute, []string{"turn:203.0.113.1:3478"})
	now := time.Unix(1_800_000_000, 0)
	issuer.now = func() time.Time { return now }

	credential, err := issuer.Issue("2d54fa38-03a6-44af-9db6-0fd672d42f95")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if credential.ExpiresAt != now.Add(10*time.Minute).Unix() {
		t.Fatalf("ExpiresAt = %d", credential.ExpiresAt)
	}

	auth := pionturn.LongTermTURNRESTAuthHandler(secret, nil)
	key, ok := auth(credential.Username, "termcall", &net.UDPAddr{})
	if !ok {
		t.Fatal("Pion rejected the generated credential")
	}
	want := pionturn.GenerateAuthKey(credential.Username, "termcall", credential.Credential)
	if !bytes.Equal(key, want) {
		t.Fatal("generated TURN authentication key does not match")
	}
}

func TestCredentialIssuerRejectsInvalidPeer(t *testing.T) {
	issuer := NewCredentialIssuer(strings.Repeat("s", 32), 10*time.Minute, []string{"turn:1.1.1.1:3478"})
	if _, err := issuer.Issue("bad/peer"); err == nil {
		t.Fatal("invalid peer unexpectedly received a credential")
	}
}

func TestServerLifecycleAndCredentialIssuance(t *testing.T) {
	port := availableTCPPort(t)
	server, err := NewServer(validTURNConfig(port))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	credential, err := server.Issue("peer-1")
	if err != nil {
		server.Close()
		t.Fatalf("Issue: %v", err)
	}
	if len(credential.URLs) != 3 || credential.Username == "" || credential.Credential == "" {
		server.Close()
		t.Fatalf("credential = %#v", credential)
	}
	if server.AllocationCount() != 0 {
		server.Close()
		t.Fatalf("allocation count = %d", server.AllocationCount())
	}
	if err := server.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := server.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestServerClosesUDPListenerWhenTCPListenFails(t *testing.T) {
	listener, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("reserve TCP port: %v", err)
	}
	defer listener.Close()
	port := uint16(listener.Addr().(*net.TCPAddr).Port)
	if _, err := NewServer(validTURNConfig(port)); err == nil {
		t.Fatal("server unexpectedly started on a reserved TCP port")
	}

	udp, err := net.ListenPacket("udp4", net.JoinHostPort("0.0.0.0", strconv.Itoa(int(port))))
	if err != nil {
		t.Fatalf("UDP listener leaked after TCP failure: %v", err)
	}
	udp.Close()
}

func TestValidateConfigRejectsUnsafeValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "private IP", mutate: func(config *Config) { config.PublicIP = net.ParseIP("10.0.0.1") }},
		{name: "zero port", mutate: func(config *Config) { config.Port = 0 }},
		{name: "empty realm", mutate: func(config *Config) { config.Realm = "" }},
		{name: "short secret", mutate: func(config *Config) { config.SharedSecret = "short" }},
		{name: "short TTL", mutate: func(config *Config) { config.CredentialTTL = time.Second }},
		{name: "relay order", mutate: func(config *Config) { config.MinRelayPort = config.MaxRelayPort + 1 }},
		{name: "quota", mutate: func(config *Config) { config.PerUserQuota = 0 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := validTURNConfig(3478)
			test.mutate(&config)
			if err := validateConfig(config); err == nil {
				t.Fatal("invalid config unexpectedly succeeded")
			}
		})
	}
}

func TestNilServerMethodsAreSafe(t *testing.T) {
	var server *Server
	if _, err := server.Issue("peer"); err == nil {
		t.Fatal("nil server issued a credential")
	}
	if server.AllocationCount() != 0 {
		t.Fatal("nil server reported allocations")
	}
	if err := server.Close(); err != nil {
		t.Fatalf("nil close: %v", err)
	}
}

func TestPeerAllowedDeniesSpecialNetworks(t *testing.T) {
	tests := []struct {
		ip      string
		allowed bool
	}{
		{ip: "127.0.0.1", allowed: false},
		{ip: "10.1.2.3", allowed: false},
		{ip: "169.254.1.1", allowed: false},
		{ip: "100.64.1.1", allowed: false},
		{ip: "192.0.2.1", allowed: false},
		{ip: "8.8.8.8", allowed: true},
	}
	for _, test := range tests {
		t.Run(test.ip, func(t *testing.T) {
			if got := peerAllowed(net.ParseIP(test.ip), false); got != test.allowed {
				t.Fatalf("peerAllowed(%s) = %v", test.ip, got)
			}
		})
	}
}

func TestPermissionHandlerPrivateOptIn(t *testing.T) {
	denyPrivate := newPermissionHandler(false)
	allowPrivate := newPermissionHandler(true)
	peer := net.ParseIP("10.1.2.3")
	if denyPrivate(nil, peer) {
		t.Fatal("private peer accepted by the default policy")
	}
	if !allowPrivate(nil, peer) {
		t.Fatal("private peer rejected after explicit opt-in")
	}
	if allowPrivate(nil, net.IPv4zero) {
		t.Fatal("unspecified peer accepted after private-network opt-in")
	}
}

func TestQuotaTracker(t *testing.T) {
	tracker := newQuotaTracker(1, 2)
	if !tracker.allow("alice", "", nil) {
		t.Fatal("first alice allocation should be allowed")
	}
	tracker.created("alice", nil)
	if tracker.allow("alice", "", nil) {
		t.Fatal("second alice allocation should be denied")
	}
	if !tracker.allow("bob", "", nil) {
		t.Fatal("first bob allocation should be allowed")
	}
	tracker.created("bob", nil)
	if tracker.allow("carol", "", nil) {
		t.Fatal("allocation above total quota should be denied")
	}
	tracker.deleted("alice")
	if !tracker.allow("carol", "", nil) {
		t.Fatal("allocation should be allowed after deletion")
	}
}

func TestQuotaTrackerReservesConcurrentAllocation(t *testing.T) {
	tracker := newQuotaTracker(1, 2)
	firstSource := &net.UDPAddr{IP: net.ParseIP("192.0.2.1"), Port: 1000}
	secondSource := &net.UDPAddr{IP: net.ParseIP("192.0.2.1"), Port: 1001}
	if !tracker.allow("alice", "", firstSource) {
		t.Fatal("first reservation should be allowed")
	}
	if !tracker.allow("alice", "", firstSource) {
		t.Fatal("retransmission from the same source should be allowed")
	}
	if tracker.allow("alice", "", secondSource) {
		t.Fatal("concurrent allocation exceeded the per-user quota")
	}
	tracker.created("alice", firstSource)
	if tracker.pendingTotal != 0 || tracker.total != 1 {
		t.Fatalf("quota state: pending=%d active=%d", tracker.pendingTotal, tracker.total)
	}
}

func validTURNConfig(port uint16) Config {
	return Config{
		PublicIP:          net.ParseIP("1.1.1.1"),
		Port:              port,
		Realm:             "termcall",
		SharedSecret:      "0123456789abcdef0123456789abcdef",
		CredentialTTL:     10 * time.Minute,
		MinRelayPort:      55000,
		MaxRelayPort:      55100,
		PerUserQuota:      2,
		TotalQuota:        10,
		AllowPrivatePeers: false,
	}
}

func availableTCPPort(t *testing.T) uint16 {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find TCP port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		t.Fatalf("release TCP port: %v", err)
	}
	return uint16(port)
}
