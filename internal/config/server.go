package config

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const minimumTURNSecretBytes = 32

type Server struct {
	HTTPAddress         string
	PublicIP            net.IP
	TURNPort            uint16
	RelayPortMin        uint16
	RelayPortMax        uint16
	MaxRoomSize         int
	TURNSharedSecret    string
	TURNCredentialTTL   time.Duration
	TURNPerUserQuota    int
	TURNTotalQuota      int
	AllowPrivatePeers   bool
	AllowedOrigins      []string
	MaxRooms            int
	MaxConnections      int
	MaxConnectionsPerIP int
	MessageRate         float64
	MessageBurst        int
}

func LoadServer(args []string, dotEnvPath string) (Server, error) {
	fileValues, err := readEnvFile(dotEnvPath)
	if err != nil {
		return Server{}, err
	}

	lookup := func(key string) (string, bool) {
		if value, ok := os.LookupEnv(key); ok {
			return value, true
		}
		value, ok := fileValues[key]
		return value, ok
	}
	return ParseServer(args, lookup)
}

func ParseServer(args []string, lookup func(string) (string, bool)) (Server, error) {
	wsBind := envString(lookup, "TERMCALL_WS_BIND", "127.0.0.1")
	wsPort, err := envInt(lookup, "TERMCALL_WS_PORT", 8080)
	if err != nil {
		return Server{}, err
	}
	turnPort, err := envInt(lookup, "TERMCALL_TURN_PORT", 3478)
	if err != nil {
		return Server{}, err
	}
	relayMin, err := envInt(lookup, "TERMCALL_RELAY_PORT_MIN", 49152)
	if err != nil {
		return Server{}, err
	}
	relayMax, err := envInt(lookup, "TERMCALL_RELAY_PORT_MAX", 65535)
	if err != nil {
		return Server{}, err
	}
	maxRoomSize, err := envInt(lookup, "TERMCALL_MAX_ROOM_SIZE", 5)
	if err != nil {
		return Server{}, err
	}
	credentialTTL, err := envDuration(lookup, "TERMCALL_TURN_CREDENTIAL_TTL", 10*time.Minute)
	if err != nil {
		return Server{}, err
	}
	perUserQuota, err := envInt(lookup, "TERMCALL_TURN_PER_USER_QUOTA", 2)
	if err != nil {
		return Server{}, err
	}
	totalQuota, err := envInt(lookup, "TERMCALL_TURN_TOTAL_QUOTA", 100)
	if err != nil {
		return Server{}, err
	}
	allowPrivatePeers, err := envBool(lookup, "TERMCALL_TURN_ALLOW_PRIVATE_PEERS", false)
	if err != nil {
		return Server{}, err
	}
	maxConnections, err := envInt(lookup, "TERMCALL_MAX_CONNECTIONS_PER_IP", 20)
	if err != nil {
		return Server{}, err
	}
	maxTotalConnections, err := envInt(lookup, "TERMCALL_MAX_CONNECTIONS", 1000)
	if err != nil {
		return Server{}, err
	}
	maxRooms, err := envInt(lookup, "TERMCALL_MAX_ROOMS", 200)
	if err != nil {
		return Server{}, err
	}
	messageRate, err := envFloat(lookup, "TERMCALL_MESSAGE_RATE", 20)
	if err != nil {
		return Server{}, err
	}
	messageBurst, err := envInt(lookup, "TERMCALL_MESSAGE_BURST", 40)
	if err != nil {
		return Server{}, err
	}

	publicIP := envString(lookup, "TERMCALL_PUBLIC_IP", "")
	turnSecret := envString(lookup, "TERMCALL_TURN_SECRET", "")
	allowedOrigins := envString(lookup, "TERMCALL_ALLOWED_ORIGINS", "")

	flags := flag.NewFlagSet("termcall-server", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&wsBind, "bind", wsBind, "HTTP bind address")
	flags.IntVar(&wsPort, "port", wsPort, "HTTP listen port")
	flags.IntVar(&turnPort, "turn-port", turnPort, "TURN server port (UDP and TCP)")
	flags.StringVar(&publicIP, "turn-ip", publicIP, "public IPv4 address advertised by TURN")
	flags.IntVar(&relayMin, "relay-min", relayMin, "TURN relay port range start")
	flags.IntVar(&relayMax, "relay-max", relayMax, "TURN relay port range end")
	flags.IntVar(&maxRoomSize, "max-room-size", maxRoomSize, "maximum peers per room")
	flags.StringVar(&turnSecret, "turn-secret", turnSecret, "TURN REST shared secret")
	flags.DurationVar(&credentialTTL, "turn-credential-ttl", credentialTTL, "TURN credential lifetime")
	flags.IntVar(&perUserQuota, "turn-per-user-quota", perUserQuota, "TURN allocations per credential")
	flags.IntVar(&totalQuota, "turn-total-quota", totalQuota, "total TURN allocations")
	flags.BoolVar(&allowPrivatePeers, "turn-allow-private-peers", allowPrivatePeers, "allow TURN peers on private/special networks")
	flags.StringVar(&allowedOrigins, "allowed-origins", allowedOrigins, "comma-separated browser WebSocket origins")
	flags.IntVar(&maxConnections, "max-connections-per-ip", maxConnections, "maximum signaling connections per IP")
	flags.IntVar(&maxTotalConnections, "max-connections", maxTotalConnections, "maximum total signaling connections")
	flags.IntVar(&maxRooms, "max-rooms", maxRooms, "maximum concurrent rooms")
	flags.Float64Var(&messageRate, "message-rate", messageRate, "signaling messages per second per connection")
	flags.IntVar(&messageBurst, "message-burst", messageBurst, "signaling message burst per connection")
	if err := flags.Parse(args); err != nil {
		return Server{}, fmt.Errorf("parse server flags: %w", err)
	}
	if flags.NArg() != 0 {
		return Server{}, fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}

	return validateServer(serverInput{
		wsBind:              wsBind,
		wsPort:              wsPort,
		publicIP:            publicIP,
		turnPort:            turnPort,
		relayMin:            relayMin,
		relayMax:            relayMax,
		maxRoomSize:         maxRoomSize,
		turnSecret:          turnSecret,
		credentialTTL:       credentialTTL,
		perUserQuota:        perUserQuota,
		totalQuota:          totalQuota,
		allowPrivatePeers:   allowPrivatePeers,
		allowedOrigins:      allowedOrigins,
		maxConnections:      maxConnections,
		maxTotalConnections: maxTotalConnections,
		maxRooms:            maxRooms,
		messageRate:         messageRate,
		messageBurst:        messageBurst,
	})
}

type serverInput struct {
	wsBind              string
	wsPort              int
	publicIP            string
	turnPort            int
	relayMin            int
	relayMax            int
	maxRoomSize         int
	turnSecret          string
	credentialTTL       time.Duration
	perUserQuota        int
	totalQuota          int
	allowPrivatePeers   bool
	allowedOrigins      string
	maxConnections      int
	maxTotalConnections int
	maxRooms            int
	messageRate         float64
	messageBurst        int
}

func validateServer(input serverInput) (Server, error) {
	bindIP := net.ParseIP(input.wsBind)
	if bindIP == nil {
		return Server{}, fmt.Errorf("HTTP bind address must be an IP address: %q", input.wsBind)
	}
	publicIP := net.ParseIP(input.publicIP)
	if !isPublicIPv4(publicIP) {
		return Server{}, errors.New("TERMCALL_PUBLIC_IP must be a public IPv4 address")
	}
	if err := validatePort("HTTP port", input.wsPort); err != nil {
		return Server{}, err
	}
	if err := validatePort("TURN port", input.turnPort); err != nil {
		return Server{}, err
	}
	if err := validatePort("relay minimum", input.relayMin); err != nil {
		return Server{}, err
	}
	if err := validatePort("relay maximum", input.relayMax); err != nil {
		return Server{}, err
	}
	if input.relayMin > input.relayMax {
		return Server{}, errors.New("relay minimum must not exceed relay maximum")
	}
	if input.maxRoomSize < 2 || input.maxRoomSize > 16 {
		return Server{}, errors.New("maximum room size must be between 2 and 16")
	}
	if !validTURNSecret(input.turnSecret) {
		return Server{}, fmt.Errorf("TURN secret must contain at least %d bytes with sufficient character diversity and must not use the old default", minimumTURNSecretBytes)
	}
	if input.credentialTTL < time.Minute || input.credentialTTL > time.Hour {
		return Server{}, errors.New("TURN credential lifetime must be between 1 minute and 1 hour")
	}
	if input.perUserQuota < 1 || input.perUserQuota > 10 {
		return Server{}, errors.New("TURN per-user quota must be between 1 and 10")
	}
	if input.totalQuota < input.perUserQuota || input.totalQuota > 10000 {
		return Server{}, errors.New("TURN total quota must be at least the per-user quota and no more than 10000")
	}
	if input.maxConnections < 1 || input.maxConnections > 1000 {
		return Server{}, errors.New("maximum connections per IP must be between 1 and 1000")
	}
	if input.maxTotalConnections < input.maxConnections || input.maxTotalConnections > 10000 {
		return Server{}, errors.New("maximum total connections must be at least the per-IP limit and no more than 10000")
	}
	if input.maxRooms < 1 || input.maxRooms > input.maxTotalConnections {
		return Server{}, errors.New("maximum rooms must be positive and no greater than the total connection limit")
	}
	if input.messageRate < 1 || input.messageRate > 1000 {
		return Server{}, errors.New("message rate must be between 1 and 1000")
	}
	if input.messageBurst < 1 || input.messageBurst > 2000 {
		return Server{}, errors.New("message burst must be between 1 and 2000")
	}

	origins, err := validateOriginList(input.allowedOrigins)
	if err != nil {
		return Server{}, err
	}

	return Server{
		HTTPAddress: net.JoinHostPort(input.wsBind, strconv.Itoa(input.wsPort)),
		PublicIP:    publicIP.To4(),
		// #nosec G115 -- validatePort rejected values outside the uint16 range above.
		TURNPort: uint16(input.turnPort),
		// #nosec G115 -- validatePort rejected values outside the uint16 range above.
		RelayPortMin: uint16(input.relayMin),
		// #nosec G115 -- validatePort rejected values outside the uint16 range above.
		RelayPortMax:        uint16(input.relayMax),
		MaxRoomSize:         input.maxRoomSize,
		TURNSharedSecret:    input.turnSecret,
		TURNCredentialTTL:   input.credentialTTL,
		TURNPerUserQuota:    input.perUserQuota,
		TURNTotalQuota:      input.totalQuota,
		AllowPrivatePeers:   input.allowPrivatePeers,
		AllowedOrigins:      origins,
		MaxRooms:            input.maxRooms,
		MaxConnections:      input.maxTotalConnections,
		MaxConnectionsPerIP: input.maxConnections,
		MessageRate:         input.messageRate,
		MessageBurst:        input.messageBurst,
	}, nil
}

func validatePort(name string, port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("%s must be between 1 and 65535", name)
	}
	return nil
}

func validTURNSecret(secret string) bool {
	if len(secret) < minimumTURNSecretBytes || secret == "termcall" {
		return false
	}
	distinct := map[byte]struct{}{}
	for index := 0; index < len(secret); index++ {
		distinct[secret[index]] = struct{}{}
	}
	return len(distinct) >= 8
}

func splitCommaList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{}
	}
	items := strings.Split(value, ",")
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}

func validateOriginList(value string) ([]string, error) {
	origins := splitCommaList(value)
	for _, origin := range origins {
		if origin == "*" || len(origin) > 256 || strings.ContainsAny(origin, " \t\r\n\x00") {
			return nil, fmt.Errorf("invalid WebSocket origin pattern %q", origin)
		}
		if strings.Contains(origin, "://") {
			parsed, err := url.Parse(origin)
			if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
				return nil, fmt.Errorf("invalid WebSocket origin pattern %q", origin)
			}
			continue
		}
		if _, err := path.Match(origin, "example.com"); err != nil {
			return nil, fmt.Errorf("invalid WebSocket origin pattern %q: %w", origin, err)
		}
	}
	return origins, nil
}

var deniedPublicIPv4Prefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
}

func isPublicIPv4(ip net.IP) bool {
	if ip == nil || ip.To4() == nil {
		return false
	}
	address, ok := netip.AddrFromSlice(ip.To4())
	if !ok || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range deniedPublicIPv4Prefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func envString(lookup func(string) (string, bool), key, fallback string) string {
	if value, ok := lookup(key); ok {
		return value
	}
	return fallback
}

func envInt(lookup func(string) (string, bool), key string, fallback int) (int, error) {
	value, ok := lookup(key)
	if !ok || value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return parsed, nil
}

func envFloat(lookup func(string) (string, bool), key string, fallback float64) (float64, error) {
	value, ok := lookup(key)
	if !ok || value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number: %w", key, err)
	}
	return parsed, nil
}

func envBool(lookup func(string) (string, bool), key string, fallback bool) (bool, error) {
	value, ok := lookup(key)
	if !ok || value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean: %w", key, err)
	}
	return parsed, nil
}

func envDuration(lookup func(string) (string, bool), key string, fallback time.Duration) (time.Duration, error) {
	value, ok := lookup(key)
	if !ok || value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration: %w", key, err)
	}
	return parsed, nil
}

func readEnvFile(path string) (map[string]string, error) {
	values := map[string]string{}
	// #nosec G304 -- callers supply an application-controlled .env path, never a network request value.
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return values, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	parsed, err := parseEnv(file)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return parsed, nil
}

func parseEnv(reader io.Reader) (map[string]string, error) {
	values := map[string]string{}
	scanner := bufio.NewScanner(reader)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("line %d has no equals sign", lineNumber)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if !validEnvKey(key) {
			return nil, fmt.Errorf("line %d has invalid key %q", lineNumber, key)
		}
		if len(value) >= 1 && (value[0] == '"' || value[0] == '\'') {
			unquoted, err := unquoteEnvValue(value)
			if err != nil {
				return nil, fmt.Errorf("line %d has invalid quoted value: %w", lineNumber, err)
			}
			value = unquoted
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read environment: %w", err)
	}
	return values, nil
}

func unquoteEnvValue(value string) (string, error) {
	if len(value) < 2 || value[len(value)-1] != value[0] {
		return "", errors.New("missing closing quote")
	}
	if value[0] == '\'' {
		inner := value[1 : len(value)-1]
		if strings.ContainsRune(inner, '\'') {
			return "", errors.New("single-quoted value contains an unescaped quote")
		}
		return inner, nil
	}
	return strconv.Unquote(value)
}

func validEnvKey(key string) bool {
	if key == "" {
		return false
	}
	for index, char := range key {
		isStart := index == 0 && (char == '_' || unicode.IsLetter(char))
		isContinuation := index > 0 && (char == '_' || unicode.IsLetter(char) || unicode.IsDigit(char))
		if !isStart && !isContinuation {
			return false
		}
	}
	return true
}
