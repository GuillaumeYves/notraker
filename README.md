# notraker

A small background daemon that shields your machine from web and email trackers.

It runs a local DNS proxy. Lookups for known tracker domains get a dead answer,
everything else passes through to a real resolver. Email tracking pixels are just
tiny images loaded from tracker domains, so the same trick covers your mail client
too. No browser extension, no per app setup, the whole machine is protected at once.

```
app or mail client
        |
        v
notraker on 127.0.0.1:53
        |-- known tracker?  ->  0.0.0.0 (nothing loads)
        `-- anything else   ->  forwarded to 1.1.1.1 / 9.9.9.9
```

## Build

Needs Go 1.22 or newer.

```
make build          # or: go build ./cmd/notraker
```

## Use

Port 53 and DNS settings are system territory, so run it elevated:
an administrator terminal on Windows, sudo on Linux and macOS.

```
notraker start      # run in the background, point system DNS at the proxy
notraker status     # is the shield up
notraker stats      # lookups seen, blocked count, top offenders
notraker stop       # stop and put system DNS back
```

`notraker run` does the same in the foreground, Ctrl+C to stop.

Flags for `run` and `start`:

```
-port n       port to serve DNS on (default 53)
-control a    address of the local control api (default 127.0.0.1:5380)
-upstream s   comma separated upstream resolvers (default 1.1.1.1:53,9.9.9.9:53)
-lists s      comma separated blocklist urls, replaces the default set
-keep-dns     leave system DNS settings alone, just serve on the port
```

## Good to know

- Blocklists come from public, community maintained sources (StevenBlack,
  Peter Lowe) and refresh daily. A cached copy keeps working offline.
- notraker always backs up your DNS settings before touching them and restores
  them on stop. If it ever dies without cleaning up, `notraker restore-dns`
  repairs things from that backup.
- Desktop mail clients like Thunderbird, Outlook and Apple Mail are fully
  covered. Gmail and some other webmail load images through their own proxy
  servers, and no local tool can block those.
- Everything listens on 127.0.0.1 only. Nothing is exposed to your network.
