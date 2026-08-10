#!/usr/bin/env bash
set -euo pipefail

# This script reads only numeric TURN settings from .env. It deliberately does
# not source the file and does not expose the loopback-only signaling port.

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
project_root="$(dirname "$script_dir")"
env_file="$project_root/.env"
dry_run=false

if [[ "${1:-}" == "--dry-run" ]]; then
    dry_run=true
elif [[ $# -ne 0 ]]; then
    echo "usage: $0 [--dry-run]" >&2
    exit 2
fi

read_env_number() {
    local key="$1"
    local fallback="$2"
    local value=""

    if [[ -f "$env_file" ]]; then
        value="$(awk -F= -v wanted="$key" '
            /^[[:space:]]*#/ { next }
            {
                name=$1
                gsub(/^[[:space:]]+|[[:space:]]+$/, "", name)
                if (name == wanted) {
                    line=$0
                    sub(/^[^=]*=/, "", line)
                    gsub(/^[[:space:]]+|[[:space:]]+$/, "", line)
                    print line
                }
            }
        ' "$env_file" | tail -n 1)"
    fi
    if [[ -z "$value" ]]; then
        value="$fallback"
    fi
    if [[ ! "$value" =~ ^[0-9]+$ ]] || (( value < 1 || value > 65535 )); then
        echo "$key must be an integer from 1 through 65535" >&2
        exit 1
    fi
    printf '%s' "$value"
}

turn_port="$(read_env_number TERMCALL_TURN_PORT 3478)"
relay_min="$(read_env_number TERMCALL_RELAY_PORT_MIN 49152)"
relay_max="$(read_env_number TERMCALL_RELAY_PORT_MAX 65535)"
if (( relay_min > relay_max )); then
    echo "TERMCALL_RELAY_PORT_MIN must not exceed TERMCALL_RELAY_PORT_MAX" >&2
    exit 1
fi

run() {
    if [[ "$dry_run" == true ]]; then
        printf 'would run:'
        printf ' %q' "$@"
        printf '\n'
        return
    fi
    "$@"
}

echo "TermCall will allow TURN $turn_port/tcp, $turn_port/udp, and relay $relay_min:$relay_max/udp."
echo "The signaling port is intentionally not opened; terminate TLS at your HTTPS listener and proxy /ws to loopback."

if command -v ufw >/dev/null 2>&1 && sudo ufw status | grep -q '^Status: active'; then
    run sudo ufw allow "$turn_port/tcp" comment "TermCall TURN TCP"
    run sudo ufw allow "$turn_port/udp" comment "TermCall TURN UDP"
    run sudo ufw allow "$relay_min:$relay_max/udp" comment "TermCall TURN relay"
    run sudo ufw reload
elif command -v iptables >/dev/null 2>&1; then
    add_iptables_rule() {
        if sudo iptables -C INPUT "$@" >/dev/null 2>&1; then
            return
        fi
        run sudo iptables -I INPUT "$@"
    }
    add_iptables_rule -p tcp --dport "$turn_port" -m comment --comment "TermCall TURN TCP" -j ACCEPT
    add_iptables_rule -p udp --dport "$turn_port" -m comment --comment "TermCall TURN UDP" -j ACCEPT
    add_iptables_rule -p udp --dport "$relay_min:$relay_max" -m comment --comment "TermCall TURN relay" -j ACCEPT
    if command -v netfilter-persistent >/dev/null 2>&1; then
        run sudo netfilter-persistent save
    else
        echo "Warning: iptables rules are not persistent; install/configure your distribution's persistence service." >&2
    fi
else
    echo "No active UFW or iptables firewall was found; no rules were changed." >&2
    exit 1
fi

echo "Host rules are ready. Apply matching inbound rules in the cloud security group."
