# bgps

Offline trekking map prep + GPS viewer in Go.

## Build

Native build:

```bash
make
```

Cross-build arm64:

```bash
make aarch64
```

Note: arm64 cross-build for `trekking_viewer` needs `aarch64-linux-gnu-gcc` and arm64 X11/OpenGL dev libs.
If building directly on arm64 device, plain `make` is simpler.

## Debian package

Native `.deb` on current machine:

```bash
make deb
```

Arm64 `.deb` from x86 host via cross-build:

```bash
make deb-aarch64
```

If building directly on arm64 device, use:

```bash
make deb
```

Output goes to `dist/`.

## Use

Prepare offline pack archive while online:

```bash
./bin/prepare_trekking --gpx ./route.gpx --out ./offline_pack.tar.gz --name "My Trek"
```

Run viewer with archive path:

```bash
./bin/trekking_viewer --pack ./offline_pack.tar.gz --port auto --baud 4800 --fullscreen
```

Viewer extracts archive to temp dir automatically.
Recorded track goes next to archive in `<pack-name>-tracks/` by default.

Optional: archive existing pack dir manually:

```bash
make pack-tar PACK_DIR=./my_pack
```

Controls:
- drag: pan
- wheel or `+` / `-`: zoom
- `F11`: fullscreen
- `F`: follow GPS
- `C`: center GPS
