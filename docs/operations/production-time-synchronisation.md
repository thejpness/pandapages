# Production time synchronisation

Accurate UTC is a Panda Pages production prerequisite. Backups, retention,
authentication expiry, logs, incident evidence, and migration cutovers all rely
on ordered timestamps. Stop an operational rollout whenever the host fails the
healthy-state gate below.

## Production design

The VPS uses `systemd-timesyncd` as its sole time client. It is enabled at boot.
Its existing effective configuration selects `ntp.hetzner.com`, which resolved
to multiple endpoints and responded during the repair. Do not run chrony,
ntpd, or OpenNTPD alongside it.

The effective configuration combines `/etc/systemd/timesyncd.conf`, drop-ins
under `/etc/systemd/timesyncd.conf.d/`, and per-link sources. Inspect it with:

```sh
sudo systemd-analyze cat-config systemd/timesyncd.conf
sudo timedatectl show-timesync --all
```

Hetzner also publishes `ntp1.hetzner.de`, `ntp2.hetzner.com`, and
`ntp3.hetzner.net`. If an explicit override is ever needed, place every
intended failover source in `NTP=`. `FallbackNTP=` is only used when no other
source is known; it is not runtime failover for an unreachable `NTP=` entry.

## Firewall requirement

On this VPS, UFW's default outgoing policy is deny. Timesync therefore needs
one outbound-only client rule:

```sh
sudo ufw allow out 123/udp comment 'systemd-timesyncd NTP client'
```

This permits UDP destination port 123 for IPv4. It also creates the IPv6 rule
when UFW is configured with `IPV6=yes`, as this host is. It does not open
inbound NTP. Verify runtime and persistent state:

```sh
sudo iptables -C ufw-user-output -p udp --dport 123 -j ACCEPT
sudo ip6tables -C ufw6-user-output -p udp --dport 123 -j ACCEPT
sudo grep -F -- '--dport 123 -j ACCEPT' /etc/ufw/user.rules
sudo grep -F -- '--dport 123 -j ACCEPT' /etc/ufw/user6.rules
```

If provider firewall egress rules are later configured, they must also permit
UDP destination port 123. Do not create an inbound UDP/123 rule.

## Healthy-state gate

Before database, backup, or deployment work, require all of:

- `NTP=yes` and `NTPSynchronized=yes`
- `systemd-timesyncd` active and enabled, with no competing daemon
- a selected source and nonzero packet count in `timedatectl timesync-status`
- packet count increasing across multiple polling intervals
- absolute NTP skew below 2 seconds, preferably below 500 ms
- no recurring resolution, timeout, permission, or timekeeping errors

```sh
timedatectl status
timedatectl timesync-status
timedatectl show -p NTP -p NTPSynchronized
systemctl is-active systemd-timesyncd
systemctl is-enabled systemd-timesyncd
sudo journalctl -u systemd-timesyncd --since '30 minutes ago' --no-pager
```

Use at least two independent trusted sources when diagnosing skew. The offset
from the selected trusted NTP peer is primary evidence and should be
corroborated by another trusted NTP source. HTTPS `Date` headers are coarse
corroboration because they have one-second resolution and can be cached.

## Diagnosing `NTPSynchronized=no`

Capture raw evidence in a root-owned mode-0700 directory and keep addresses out
of tickets and pull requests.

1. Inspect `timedatectl`, the timesync status, effective configuration, and
   service journal.
2. Confirm exactly one time client is active and enabled.
3. Resolve every configured source and inspect DNS/network-online state.
4. Inspect UFW, nftables, and iptables output policy.
5. Check that no conflicting service owns UDP/123. A client does not normally
   listen on local port 123.
6. Send a valid NTP request. `nc -u` alone does not prove a response arrived.
7. Local `EPERM` on send indicates host policy. If outbound packets receive no
   replies, investigate the peer, upstream/provider egress, routing, return
   path, and local stateful filtering.
8. If replies arrive but drift continues, inspect virtualisation clocksource
   and kernel timekeeping messages.

Useful commands:

```sh
sudo ss -H -ulpn 'sport = :123'
sudo ufw status verbose
sudo nft list ruleset
sudo iptables -S OUTPUT
resolvectl status
getent ahosts ntp.hetzner.com
systemd-detect-virt
cat /sys/devices/system/clocksource/clocksource0/current_clocksource
```

## Safe recovery

Before restoring connectivity or restarting the daemon:

1. Confirm no migration, deployment, backup, restore, or other
   wall-clock-sensitive operation is active.
2. Check for active or long-running database transactions.
3. Back up every file to be changed into the protected evidence directory.
4. Record UTC, service state, firewall state, and measured skew.

The 2026-07-13 production incident was caused by UFW default-deny output with
no UDP/123 allowance. DNS, the provider source, and timesyncd were healthy, so
no source configuration or alternate daemon was needed. After applying only
the evidenced firewall correction, use the daemon's supported path:

```sh
sudo systemctl restart systemd-timesyncd
```

Do not manually use `date -s` or `timedatectl set-time` while NTP can be
restored. Timesyncd steps large offsets and slews small ones. A backward step
can reorder wall-clock timestamps even though monotonic timers continue.
Do not restart PostgreSQL or Panda Pages solely because time was corrected.

Observe at least three successive poll intervals. Record source, packet counts,
offset, delay, poll interval, and logs; one packet is not stability evidence.

## Persistence and rollback

Verify reboot persistence without rebooting:

```sh
systemctl is-enabled systemd-timesyncd
systemctl is-enabled ufw
sudo grep -F -- '--dport 123 -j ACCEPT' /etc/ufw/user.rules
sudo grep -F -- '--dport 123 -j ACCEPT' /etc/ufw/user6.rules
```

A future maintenance reboot still needs a post-boot synchronisation check.
Before edits, preserve `/etc/default/ufw`, both UFW user rule files, and any
changed timesync configuration. If this repair makes synchronisation worse,
first verify and remove only the exact added rule with
`sudo ufw delete allow out 123/udp`, reload UFW, and restart timesyncd. Restore
whole rule files only as a last resort after confirming their hashes and that
no intervening firewall edits would be lost. Never add a competing daemon or
remove the rollout time gate.

## References

- [Hetzner NTP servers](https://docs.hetzner.com/robot/dedicated-server/security/ntp-servers/)
- [systemd-timesyncd](https://www.freedesktop.org/software/systemd/man/latest/systemd-timesyncd.service.html)
- [timesyncd configuration](https://www.freedesktop.org/software/systemd/man/latest/timesyncd.conf.html)
- [timedatectl](https://www.freedesktop.org/software/systemd/man/latest/timedatectl.html)
