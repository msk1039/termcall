package turn

import (
	"crypto/hmac"
	"crypto/sha1" // #nosec G505 -- the TURN REST credential protocol requires HMAC-SHA1.
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/msk1039/termcall/internal/protocol"
	"github.com/pion/logging"
	pionturn "github.com/pion/turn/v4"
)

type Config struct {
	PublicIP          net.IP
	Port              uint16
	Realm             string
	SharedSecret      string
	CredentialTTL     time.Duration
	MinRelayPort      uint16
	MaxRelayPort      uint16
	PerUserQuota      int
	TotalQuota        int
	AllowPrivatePeers bool
}

type Server struct {
	turnServer *pionturn.Server
	issuer     *CredentialIssuer
	closeOnce  sync.Once
	closeErr   error
}

func NewServer(config Config) (*Server, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}

	listenAddress := net.JoinHostPort("0.0.0.0", strconv.Itoa(int(config.Port)))
	udpListener, err := net.ListenPacket("udp4", listenAddress)
	if err != nil {
		return nil, fmt.Errorf("listen for TURN UDP: %w", err)
	}

	tcpListener, err := net.Listen("tcp4", listenAddress)
	if err != nil {
		_ = udpListener.Close()
		return nil, fmt.Errorf("listen for TURN TCP: %w", err)
	}

	issuer := NewCredentialIssuer(
		config.SharedSecret,
		config.CredentialTTL,
		turnURLs(config.PublicIP, config.Port),
	)
	quotas := newQuotaTracker(config.PerUserQuota, config.TotalQuota)
	permissionHandler := newPermissionHandler(config.AllowPrivatePeers)
	events := pionturn.EventHandler{
		OnAllocationCreated: func(
			sourceAddress net.Addr,
			_ net.Addr,
			_ string,
			username string,
			_ string,
			_ net.Addr,
			_ int,
		) {
			quotas.created(username, sourceAddress)
		},
		OnAllocationDeleted: func(_ net.Addr, _ net.Addr, _ string, username, _ string) {
			quotas.deleted(username)
		},
	}

	serverConfig := pionturn.ServerConfig{
		Realm:         config.Realm,
		AuthHandler:   pionturn.LongTermTURNRESTAuthHandler(config.SharedSecret, nil),
		QuotaHandler:  quotas.allow,
		EventHandler:  events,
		LoggerFactory: logging.NewDefaultLoggerFactory(),
		PacketConnConfigs: []pionturn.PacketConnConfig{
			{
				PacketConn: udpListener,
				RelayAddressGenerator: &pionturn.RelayAddressGeneratorPortRange{
					RelayAddress: config.PublicIP,
					Address:      "0.0.0.0",
					MinPort:      config.MinRelayPort,
					MaxPort:      config.MaxRelayPort,
				},
				PermissionHandler: permissionHandler,
			},
		},
		ListenerConfigs: []pionturn.ListenerConfig{
			{
				Listener: tcpListener,
				RelayAddressGenerator: &pionturn.RelayAddressGeneratorPortRange{
					RelayAddress: config.PublicIP,
					Address:      "0.0.0.0",
					MinPort:      config.MinRelayPort,
					MaxPort:      config.MaxRelayPort,
				},
				PermissionHandler: permissionHandler,
			},
		},
	}

	turnServer, err := pionturn.NewServer(serverConfig)
	if err != nil {
		_ = tcpListener.Close()
		_ = udpListener.Close()
		return nil, fmt.Errorf("create TURN server: %w", err)
	}

	log.Printf(
		"TURN listening on UDP/TCP %d with relay ports %d-%d",
		config.Port,
		config.MinRelayPort,
		config.MaxRelayPort,
	)
	return &Server{
		turnServer: turnServer,
		issuer:     issuer,
	}, nil
}

func (s *Server) Issue(peerID string) (protocol.ICEServer, error) {
	if s == nil || s.issuer == nil {
		return protocol.ICEServer{}, errors.New("TURN credential issuer is unavailable")
	}
	return s.issuer.Issue(peerID)
}

func (s *Server) AllocationCount() int {
	if s == nil || s.turnServer == nil {
		return 0
	}
	return s.turnServer.AllocationCount()
}

func (s *Server) Close() error {
	if s == nil || s.turnServer == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		s.closeErr = s.turnServer.Close()
	})
	return s.closeErr
}

func validateConfig(config Config) error {
	if config.PublicIP == nil || config.PublicIP.To4() == nil || !peerAllowed(config.PublicIP, false) {
		return errors.New("TURN public IP must be a public IPv4 address")
	}
	if config.Port == 0 {
		return errors.New("TURN port must not be zero")
	}
	if config.Realm == "" {
		return errors.New("TURN realm must not be empty")
	}
	if !validSharedSecret(config.SharedSecret) {
		return errors.New("TURN shared secret must contain at least 32 bytes with sufficient character diversity")
	}
	if config.CredentialTTL < time.Minute || config.CredentialTTL > time.Hour {
		return errors.New("TURN credential lifetime must be between 1 minute and 1 hour")
	}
	if config.MinRelayPort == 0 || config.MinRelayPort > config.MaxRelayPort {
		return errors.New("TURN relay port range is invalid")
	}
	if config.PerUserQuota < 1 || config.TotalQuota < config.PerUserQuota {
		return errors.New("TURN allocation quotas are invalid")
	}
	return nil
}

func validSharedSecret(secret string) bool {
	if len(secret) < 32 || secret == "termcall" {
		return false
	}
	distinct := map[byte]struct{}{}
	for index := 0; index < len(secret); index++ {
		distinct[secret[index]] = struct{}{}
	}
	return len(distinct) >= 8
}

func turnURLs(publicIP net.IP, port uint16) []string {
	host := publicIP.String()
	return []string{
		fmt.Sprintf("stun:%s:%d", host, port),
		fmt.Sprintf("turn:%s:%d", host, port),
		fmt.Sprintf("turn:%s:%d?transport=tcp", host, port),
	}
}

type CredentialIssuer struct {
	sharedSecret string
	ttl          time.Duration
	urls         []string
	now          func() time.Time
}

func NewCredentialIssuer(sharedSecret string, ttl time.Duration, urls []string) *CredentialIssuer {
	return &CredentialIssuer{
		sharedSecret: sharedSecret,
		ttl:          ttl,
		urls:         append([]string{}, urls...),
		now:          time.Now,
	}
}

func (i *CredentialIssuer) Issue(peerID string) (protocol.ICEServer, error) {
	if err := protocol.ValidatePeerID(peerID); err != nil {
		return protocol.ICEServer{}, fmt.Errorf("issue TURN credential: %w", err)
	}
	expiresAt := i.now().Add(i.ttl).Unix()
	username := strconv.FormatInt(expiresAt, 10) + ":" + peerID

	mac := hmac.New(sha1.New, []byte(i.sharedSecret))
	if _, err := mac.Write([]byte(username)); err != nil {
		return protocol.ICEServer{}, fmt.Errorf("sign TURN credential: %w", err)
	}
	credential := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return protocol.ICEServer{
		URLs:       append([]string{}, i.urls...),
		Username:   username,
		Credential: credential,
		ExpiresAt:  expiresAt,
	}, nil
}

type quotaTracker struct {
	mu            sync.Mutex
	byUser        map[string]int
	pending       map[string]string
	pendingByUser map[string]int
	total         int
	pendingTotal  int
	perUser       int
	maxTotal      int
}

const quotaReservationTTL = 5 * time.Second

func newQuotaTracker(perUser, maxTotal int) *quotaTracker {
	return &quotaTracker{
		byUser:        map[string]int{},
		pending:       map[string]string{},
		pendingByUser: map[string]int{},
		perUser:       perUser,
		maxTotal:      maxTotal,
	}
}

func (q *quotaTracker) allow(username, _ string, sourceAddress net.Addr) bool {
	key := quotaReservationKey(username, sourceAddress)
	q.mu.Lock()
	if _, exists := q.pending[key]; exists {
		q.mu.Unlock()
		return true
	}
	if q.total+q.pendingTotal >= q.maxTotal || q.byUser[username]+q.pendingByUser[username] >= q.perUser {
		q.mu.Unlock()
		return false
	}
	q.pending[key] = username
	q.pendingByUser[username]++
	q.pendingTotal++
	q.mu.Unlock()

	time.AfterFunc(quotaReservationTTL, func() {
		q.releasePending(key, username)
	})
	return true
}

func (q *quotaTracker) created(username string, sourceAddress net.Addr) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.removePendingLocked(quotaReservationKey(username, sourceAddress), username)
	q.byUser[username]++
	q.total++
}

func (q *quotaTracker) releasePending(key, username string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.removePendingLocked(key, username)
}

func (q *quotaTracker) removePendingLocked(key, username string) {
	if _, exists := q.pending[key]; !exists {
		return
	}
	delete(q.pending, key)
	if q.pendingByUser[username] > 1 {
		q.pendingByUser[username]--
	} else {
		delete(q.pendingByUser, username)
	}
	if q.pendingTotal > 0 {
		q.pendingTotal--
	}
}

func quotaReservationKey(username string, sourceAddress net.Addr) string {
	if sourceAddress == nil {
		return username + "|<nil>"
	}
	return username + "|" + sourceAddress.Network() + "|" + sourceAddress.String()
}

func (q *quotaTracker) deleted(username string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.byUser[username] > 1 {
		q.byUser[username]--
	} else {
		delete(q.byUser, username)
	}
	if q.total > 0 {
		q.total--
	}
}

var deniedPeerPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("2001:db8::/32"),
}

func newPermissionHandler(allowPrivate bool) pionturn.PermissionHandler {
	return func(_ net.Addr, peerIP net.IP) bool {
		return peerAllowed(peerIP, allowPrivate)
	}
}

func peerAllowed(peerIP net.IP, allowPrivate bool) bool {
	address, ok := netip.AddrFromSlice(peerIP)
	if !ok {
		return false
	}
	address = address.Unmap()
	if allowPrivate {
		return !address.IsUnspecified() && !address.IsMulticast()
	}
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range deniedPeerPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}
