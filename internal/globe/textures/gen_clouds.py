#!/usr/bin/env python3
"""Generate earth_clouds.txt — a sparse overlay of natural cloud blobs.

Same dimensions and orientation as earth.txt (300x75, lon=-180 at the LEFT of
the file, north pole at the top). After globe.parseTexture reverses each row,
the stored array[x=0] corresponds to the file's LAST column, so this file uses
the *same* left-edge-is-180W convention as earth.txt and clouds line up with
the day-texture landmasses.

Design goals:
  - Sparse (large clear regions, ~10% coverage).
  - Natural swirling shapes — no latitudinal banding, no seams.
  - Cloud glyphs chosen from a configurable palette of density characters,
    darkest → brightest. Empty cells are spaces, meaning "no cloud here".

Technique: 2D Perlin-style gradient noise with wraparound on the longitude
axis (so the texture tiles seamlessly as the globe spins), plus a second
high-frequency octave, plus a smooth domain warp on the sample coordinates
to break up any residual grid alignment. The cloud amplitude is then gently
latitude-weighted (more near equator, very little near poles) but with the
weight itself jittered so it does NOT produce visible bands.
"""

import math
import random
import sys

W, H = 300, 75
SEED = 11

# Density palette, darkest → brightest. Edit this to change which characters
# appear in the texture; the runtime color comes from main.go's cloud palette.
PALETTE = [".", ":", "'", ",", "w", "W"]

# Coverage target — roughly what fraction of cells should be non-empty.
COVERAGE = 0.12

# Base amplitude below which a cell stays clear (no cloud).
# Tuned so that roughly COVERAGE of cells survive after latitude weighting.
THRESHOLD = 0.22

random.seed(SEED)


# ─────────────────────────── gradient noise (wraparound) ─────────────────


def _fade(t):
    return t * t * t * (t * (t * 6 - 15) + 10)


def _lerp(a, b, t):
    return a + (b - a) * t


def _grad(hx, hy, dx, dy):
    """Pseudo-gradient: gives a scalar in [-1,1] based on grid cell + offset."""
    # Use a hash of (hx,hy) to pick one of 8 directions, then dot product.
    n = (hx * 374761393 + hy * 668265263) & 0xFFFFFFFF
    n = (n ^ (n >> 13)) * 1274126177 & 0xFFFFFFFF
    n = n ^ (n >> 16)
    ang = (n & 7) * (math.pi / 4)
    return dx * math.cos(ang) + dy * math.sin(ang)


def perlin2(cells_x, cells_y, wrap_x):
    """Perlin-style noise field of size (W,H). If wrap_x, the field tiles
    seamlessly along the x axis by wrapping the grid."""
    field = [[0.0] * H for _ in range(W)]
    # Pre-generate a coarse grid of pseudo-random gradients via the hash.
    for x in range(W):
        for y in range(H):
            gx = x / W * cells_x
            gy = y / H * cells_y
            x0 = int(gx) % cells_x if wrap_x else min(int(gx), cells_x - 1)
            y0 = int(gy)
            x1 = (x0 + 1) % cells_x if wrap_x else min(x0 + 1, cells_x - 1)
            y1 = min(y0 + 1, cells_y - 1)
            fx = gx - int(gx)
            fy = gy - int(gy)
            # Need fractional offsets relative to each grid corner.
            # For wrapped x, the corner's x-coord is x0 (or x1) but the
            # fraction still measures distance from x0.
            dx0 = fx
            dx1 = fx - 1.0
            dy0 = fy
            dy1 = fy - 1.0
            # Compute gradient contributions. We need consistent hashing per
            # grid cell — use absolute grid indices, accounting for wrap.
            ax0 = int(gx) if not wrap_x else int(gx) % cells_x
            ax1 = (ax0 + 1) % cells_x if wrap_x else ax0 + 1
            ay0 = y0
            ay1 = y1
            n00 = _grad(ax0, ay0, dx0, dy0)
            n10 = _grad(ax1, ay0, dx1, dy0)
            n01 = _grad(ax0, ay1, dx0, dy1)
            n11 = _grad(ax1, ay1, dx1, dy1)
            u = _fade(fx)
            v = _fade(fy)
            field[x][y] = _lerp(_lerp(n00, n10, u), _lerp(n01, n11, u), v)
    # Normalize to roughly [0,1]
    lo = min(min(row) for row in field)
    hi = max(max(row) for row in field)
    span = (hi - lo) or 1.0
    return [[(v - lo) / span for v in row] for row in field]


def render():
    # Two octaves of wrapping Perlin noise, plus domain warp for organic swirl.
    base = perlin2(8, 5, wrap_x=True)  # large slow blobs
    fine = perlin2(22, 13, wrap_x=True)  # high-frequency detail
    warp_x = perlin2(5, 3, wrap_x=True)
    warp_y = perlin2(5, 3, wrap_x=True)

    out = [[" " for _ in range(W)] for _ in range(H)]
    for x in range(W):
        for y in range(H):
            # Domain warp: offset the sample point by a low-frequency field.
            # This breaks the grid alignment of the underlying noise.
            wx = (warp_x[x][y] - 0.5) * 0.20
            wy = (warp_y[x][y] - 0.5) * 0.20
            xs = min(int(x + wx * W), W - 1)
            ys = max(0, min(int(y + wy * H), H - 1))

            v = base[xs][ys] * 0.70 + fine[xs][ys] * 0.30

            # Latitude weighting — equator ~1.0, poles ~0.15, with a small
            # latitude-dependent jitter so it never forms a clean band.
            lat = 90 - (y / (H - 1)) * 180
            lw = 0.20 + 0.80 * (math.cos(lat * math.pi / 180) ** 2)
            jitter = 0.15 * (fine[x][y] - 0.5)
            lw = max(0.08, lw + jitter)

            v *= lw
            if v < THRESHOLD:
                continue
            # Map amplitude above threshold → glyph index in PALETTE.
            t = (v - THRESHOLD) / (1.0 - THRESHOLD)
            idx = min(len(PALETTE) - 1, int(t * len(PALETTE)))
            out[y][x] = PALETTE[idx]
    return out


grid = render()
# Honor COVERAGE by trimming the densest cells if we overshoot.
non_empty = sum(1 for row in grid for c in row if c != " ")
max_keep = int(COVERAGE * W * H)
if non_empty > max_keep:
    # Keep only the brightest cells: collect (amplitude, x, y) and threshold.
    cand = []
    for y, row in enumerate(grid):
        for x, c in enumerate(row):
            if c != " ":
                cand.append((PALETTE.index(c), x, y))
    cand.sort(reverse=True)
    keep = set((x, y) for _, x, y in cand[:max_keep])
    for y in range(H):
        for x in range(W):
            if (x, y) not in keep:
                grid[y][x] = " "
for row in grid:
    sys.stdout.write("".join(row) + "\n")
