#!/bin/bash
set -euo pipefail

# Load .env from the same directory as this script, or from the project root
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

if [ -f "$PROJECT_ROOT/.env" ]; then
    set -a
    source "$PROJECT_ROOT/.env"
    set +a
elif [ -f ".env" ]; then
    set -a
    source ".env"
    set +a
else
    echo "Warning: No .env file found. Using defaults."
fi

# Apply defaults for any unset variables
WS_PORT="${TERMCALL_WS_PORT:-8080}"
TURN_PORT="${TERMCALL_TURN_PORT:-3478}"
RELAY_MIN="${TERMCALL_RELAY_PORT_MIN:-49152}"
RELAY_MAX="${TERMCALL_RELAY_PORT_MAX:-65535}"

echo "=== TermCall Firewall Setup ==="
echo "  WebSocket port:  $WS_PORT/tcp"
echo "  TURN port:       $TURN_PORT/tcp+udp"
echo "  Relay range:     $RELAY_MIN-$RELAY_MAX/udp"
echo ""

# Detect firewall tool
if command -v ufw &> /dev/null; then
    echo "Configuring UFW..."
    sudo ufw allow "$WS_PORT/tcp" comment "TermCall WebSocket"
    sudo ufw allow "$TURN_PORT/tcp" comment "TermCall TURN TCP"
    sudo ufw allow "$TURN_PORT/udp" comment "TermCall TURN UDP"
    sudo ufw allow "$RELAY_MIN:$RELAY_MAX/udp" comment "TermCall TURN relay"
    sudo ufw reload
    echo "UFW rules applied."
fi

if command -v iptables &> /dev/null; then
    echo "Configuring iptables..."
    sudo iptables -I INPUT -p tcp --dport "$WS_PORT" -j ACCEPT -m comment --comment "TermCall WebSocket"
    sudo iptables -I INPUT -p tcp --dport "$TURN_PORT" -j ACCEPT -m comment --comment "TermCall TURN TCP"
    sudo iptables -I INPUT -p udp --dport "$TURN_PORT" -j ACCEPT -m comment --comment "TermCall TURN UDP"
    sudo iptables -I INPUT -p udp --dport "$RELAY_MIN:$RELAY_MAX" -j ACCEPT -m comment --comment "TermCall TURN relay"

    if command -v netfilter-persistent &> /dev/null; then
        sudo netfilter-persistent save
    fi
    echo "iptables rules applied."
fi

echo ""
echo "Done! Ports are now open:"
echo "  $WS_PORT/tcp, $TURN_PORT/tcp+udp, $RELAY_MIN-$RELAY_MAX/udp"
