#!/bin/bash

echo "Opening TURN server ports in UFW..."
sudo ufw allow 8080/tcp
sudo ufw allow 8443/tcp
sudo ufw allow 8443/udp
sudo ufw allow 3478/tcp
sudo ufw allow 3478/udp
sudo ufw allow 50000:50050/udp
sudo ufw reload

echo "Opening TURN server ports in iptables (as backup)..."
sudo iptables -I INPUT -p tcp --dport 8080 -j ACCEPT
sudo iptables -I INPUT -p tcp --dport 8443 -j ACCEPT
sudo iptables -I INPUT -p udp --dport 8443 -j ACCEPT
sudo iptables -I INPUT -p tcp --dport 3478 -j ACCEPT
sudo iptables -I INPUT -p udp --dport 3478 -j ACCEPT
sudo iptables -I INPUT -p udp --dport 50000:50050 -j ACCEPT

# Save iptables rules so they persist across reboots
if command -v netfilter-persistent &> /dev/null; then
    sudo netfilter-persistent save
fi

echo "Done! Port 8443 and the 50000-50050 relay range are now fully open on the OS level."
