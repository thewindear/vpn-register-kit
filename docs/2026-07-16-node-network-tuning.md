# Node Network Tuning

`install-node.sh` applies Linux, nginx, and sing-box concurrency tuning during node setup so new VPN nodes accept larger HTTP/TCP bursts without manual post-install changes.

The installer writes persistent kernel TCP settings to `/etc/sysctl.d/99-vpn-node-network-tuning.conf`, raises nginx systemd limits with `/etc/systemd/system/nginx.service.d/limits.conf`, patches `/etc/nginx/nginx.conf` for worker capacity, and writes nginx site listeners with `backlog=65535`.

The default nginx site is patched when present because nginx can keep the effective public `80/tcp` listen backlog at the default value if the default server owns the socket options.

sing-box server config is generated with lower-volume `warn` logging, TCP Fast Open and MPTCP enabled on Trojan and Shadowsocks inbounds, and TCP Fast Open, MPTCP, and a `10s` connect timeout on the direct outbound. The installer also writes `/etc/systemd/system/sing-box.service.d/limits.conf` with `LimitNOFILE=infinity` and `TasksMax=infinity`.
