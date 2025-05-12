Demonstration purposes only

## Motivation

- With 2 machines, both behind different NAT/firewalls, proxy between them and reach the internal network without requiring a central server or exposing my IP address
- In censorship heavy countries (e.g. China, Iran, etc), double hop a VPN connection to relays located in friendlier countries to avoid suspicion while appearing as standard TLS
- Serve content to the outside world from within a NAT similar to ngrok.

## How it works

- syncthing but rather than files we send arbitrary data
