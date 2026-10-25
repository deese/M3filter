# m3filter

Single-binary CLI to filter M3U/M3U8 playlists by substring, glob or regex.

```
m3filter [flags] [input.m3u | http(s)://url]   # stdin when no input is given
```

Patterns are `[field:]pattern`. Fields: `name` (default), `group`, `id`, `logo`, `url`, `any`.
An entry is kept if it matches any `-i` pattern (or there are none) and no `-x` pattern.
Matching is case-insensitive unless `-c` is set. Modes (`-m`): `contains` (default), `glob`, `regex` (Go RE2).

```
m3filter -m regex -i 'group:^Sports' -x 'name:adult|xxx' in.m3u -o out.m3u
m3filter -m glob -i 'ESPN*' -i 'Fox*' in.m3u
m3filter -groups in.m3u
```

Releases (Linux, macOS, Windows; amd64 and arm64) are built on `v*` tags.
