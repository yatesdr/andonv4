# Andon Canvas — Component Rendering Notes

## Architecture

The rendering code lives in `static/render.js`, a shared ES module imported by both pages:
- **templates/designer.html** — Designer/editor (960x540 CSS at 2x scale). Adds selection handles, drag/drop, modals, palette.
- **templates/display.html** — Full-screen TV display (served at `/screens/{slug}`). Auto-scales to viewport, loads layout JSON, connects to SSE for live updates.

All shape and child IDs are UUIDs generated via `crypto.randomUUID()`, enabling stable keying for SSE updates.

## Canvas Setup
- Canvas: 1920x1080 internal resolution
- Editor displayed at 960x540 CSS (SCALE = 2)
- Display auto-scales to fill viewport maintaining 16:9
- Background: pure white (#ffffff)
- Flash timer: shared `flashOn` boolean toggled every 300ms via setInterval
- Flash effect: background color swaps to white (#ffffff); borders remain solid

---

## Render Order (4 passes)

The render loop (`renderFrame()` in render.js) runs via `requestAnimationFrame` in strict order:

1. **Pass 1 — Arrows** (bottom layer, behind everything)
2. **Pass 2 — Standard shapes** (process, press, header, image, buffer, multibuffer) — z-order follows array position
3. **Pass 3 — Final** (renders above standard shapes)
4. **Pass 4 — Status** (topmost layer, overlays everything)

Shapes skipped in Pass 2: `arrow`, `status`, `final` — each has its own dedicated pass.

Selection handles (including arrow endpoint handles) are drawn after `renderFrame()` by the editor only.

---

## Component Configs & Rendering

### 1. Process (`type: 'process'`)

**Config:**
```json
{
  "x": 0, "y": 0, "w": 160, "h": 120,
  "backgroundColor": "#00FF00",
  "borderColor": "#000000",
  "borderWidth": 0,
  "textColor": "#000000",
  "flashing": false,
  "glow": "orange | blue | null",
  "chip": "red | green | null",
  "text": "99",
  "plc": "",
  "count_tag": "",
  "state_tag": "",
  "children": [
    { "id": "<uuid>", "type": "diamond",   "relX": -0.22, "relY": -0.05, "color": "#e53935" },
    { "id": "<uuid>", "type": "gear",      "relX": -0.22, "relY": 0.225, "color": "#e53935" },
    { "id": "<uuid>", "type": "stopwatch", "relX": -0.22, "relY": 0.5, "color": "#e53935" },
    { "id": "<uuid>", "type": "x",         "relX": -0.22, "relY": 0.775, "color": "#e53935" },
    { "id": "<uuid>", "type": "rabbit",    "relX": -0.22, "relY": 1.05, "color": "#e53935" },
    { "id": "<uuid>", "type": "label",     "relX": 0.67,  "relY": 1.18,  "color": "#000000", "text": "OP-100", "scale": 1 }
  ]
}
```

**Rendering steps (in order):**
1. **Glow** (if `glow` is set): Draw a rect at sqrt(1.2) scale (~120% area) in glow color (`orange: #FF6A00`, `blue: #00D4FF`) with shadowBlur = `max(10, sqrt(w*h)*0.1)`. Three passes at alpha 0.9, 0.7, 0.5 for intensity.
2. **Flash**: If `flashing && !flashOn`, fill is white (#ffffff) instead of backgroundColor.
3. **Fill**: Rectangle with `backgroundColor` (or white if flashing).
4. **Border**: Stroke rect, width = `borderWidth || max(4, sqrt(w*h)*0.04)`, color = `borderColor || #000`, lineJoin = miter. Border always draws solid regardless of flash state.
5. **Chip** (if set): Perfect square in upper-left corner inside border, side = `sqrt(0.05 * w * h)` (~5% area). Color: red=#FF0000, green=#00FF00. No border on chip.
6. **Text**: Centered, font size = `max(14, min(h*0.4, w*0.4))`, bold system font, color = `textColor || #000`.
7. **Children**: Each child positioned at `(x + relX*h, y + relY*h)`, base size = `h * 0.28`, actual size = base * `iconScale` (default 1). relX/relY are fractions of parent height so everything scales proportionally.

**Background color options:** green (#00FF00), orange (#FF8C00), gray/inactive (#AAAAAA), violet (#9B59B6).

**Interactions:** Move, resize (corner handles), child drag, label inline edit (dblclick), label resize handles, properties modal (dblclick body), Alt+drag clone, Delete key.

---

### 2. Press (`type: 'press'`)

**Config:**
```json
{
  "x": 0, "y": 0, "w": 160, "h": 128,
  "backgroundColor": "#00FF00",
  "borderColor": "#000000",
  "borderWidth": 0,
  "textColor": "#000000",
  "flashing": false,
  "glow": "orange | blue | null",
  "chip": "red | green | null",
  "text": "999",
  "plc": "",
  "count_tag": "",
  "state_tag": "",
  "children": [
    { "id": "<uuid>", "type": "pm",        "relX": 0.025,  "relY": 1.25, "color": "#e53935", "iconScale": 1.0 },
    { "id": "<uuid>", "type": "wrench",    "relX": 0.325,  "relY": 1.25, "color": "#e53935", "iconScale": 1.0 },
    { "id": "<uuid>", "type": "gear",      "relX": 0.625,  "relY": 1.25, "color": "#e53935", "iconScale": 1.0 },
    { "id": "<uuid>", "type": "stopwatch", "relX": 0.925,  "relY": 1.25, "color": "#e53935", "iconScale": 1.0 },
    { "id": "<uuid>", "type": "x",         "relX": 1.225,  "relY": 1.25, "color": "#e53935", "iconScale": 1.0 },
    { "id": "<uuid>", "type": "label",     "relX": 0.625,  "relY": 1.55, "color": "#000000", "text": "PR-100", "scale": 1.3 },
    { "id": "<uuid>", "type": "preflight", "relX": -0.45,  "relY": 0.5,  "backgroundColor": "#00FF00", "flashing": false },
    { "id": "<uuid>", "type": "feeder",    "relX": 0.625,  "relY": -0.70, "backgroundColor": "#00FF00", "flashing": false }
  ]
}
```

**Rendering:** Identical to Process — both use `drawRectangle()`. Same glow, flash, fill, border, chip, text, children logic.

**Aspect ratio:** 5:4 (default 160x128). Resize is constrained to maintain 5:4. Horizontal center for children = `w / (2*h) = 0.625` in relX.

**Default children layout:**
- 5 status icons in a horizontal row below the parent (relY: 1.25), spaced at 0.3 intervals centered under the parent
- Label below the icon row (relY: 1.55), scale 1.3
- Preflight rectangle to the left of the parent (relX: -0.45), clear of glow radius
- Feeder rectangle above the parent (relX: 0.625, relY: -0.70), clear of glow radius

**Interactions:** Same as Process. Properties modal shared with process.

---

### 3. Single Buffer (`type: 'buffer'`)

**Config:**
```json
{
  "x": 0, "y": 0, "w": 46, "h": 46,
  "backgroundColor": "#ffffff",
  "plc": "",
  "tag": ""
}
```

**Rendering:**
1. **Fill**: Ellipse with `backgroundColor` (default white, option for green #00FF00).
2. **Border**: Stroked ellipse, black, width = `max(4, sqrt(w*h)*0.04)`.

**Constraints:** Resize locked to 1:1 (perfect circle) — uses `max(nw, nh)` for both dimensions.

**Interactions:** Move, resize (constrained circle), properties modal (dblclick, PLC + Tag), Alt+drag clone, Delete.

---

### 4. Multi Buffer (`type: 'multibuffer'`)

**Config:**
```json
{
  "x": 0, "y": 0, "w": 115, "h": 46,
  "backgroundColor": "#ffffff",
  "textColor": "#000000",
  "text": "999",
  "plc": "",
  "count_tag": ""
}
```

**Rendering:**
1. **Fill**: White rectangle.
2. **Border**: Black stroke, width = `max(4, sqrt(w*h)*0.04)`, lineJoin=miter.
3. **Text**: Centered, font size = `max(10, h*0.8)`, bold system font, black.

**Constraints:** Resize locked to 5:2 aspect ratio.

**Interactions:** Move, resize (constrained 5:2), properties modal (dblclick, PLC + Count Tag), Alt+drag clone, Delete.

---

### 5. Flow Arrow (`type: 'arrow'`)

**Config:**
```json
{
  "x1": 0, "y1": 0,
  "x2": 150, "y2": 0,
  "thickness": 6,
  "showHead": true
}
```

**Rendering:**
1. **Line**: Black stroke from (x1,y1) toward (x2,y2). If showHead, line stops at 60% of headLen from head. lineCap=butt.
2. **Arrowhead** (if `showHead !== false`): Filled black triangle at (x2,y2). headLen = thickness*5, angle spread = +/-0.45 radians.

**Note:** Arrows always render in Pass 1 (behind everything). Arrow selection handles render after all passes (editor only).

**Selection handles:**
- **Head** (at x2,y2): Square, dark
- **Tail** (at x1,y1): Square, dark
- **Scale** (midpoint, perpendicular offset): Blue diamond (#4a90d9), rotated 45deg. Offset = `thickness*2 + 15` perpendicular to arrow direction.

**Endpoint dragging:** 15-degree angle snapping. `SNAP_ANGLE = PI/12`. Snapped angle = `round(atan2(dy,dx) / SNAP_ANGLE) * SNAP_ANGLE`. Distance preserved.

**Scale dragging:** thickness = perpendicular distance from mouse to line segment, clamped 2-40.

**Hit testing:** Point-to-segment distance <= `max(10, thickness*2)`.

**Interactions:** Move body, drag head/tail with snapping, drag scale handle, dblclick toggles showHead, Alt+drag clone, Delete.

---

### 6. Header (`type: 'header'`)

**Config:**
```json
{
  "x": 0, "y": 0, "w": 1920, "h": 120,
  "text": "HEADER",
  "textX": 0.5,
  "textY": 0.5
}
```

**Rendering:**
1. **Gradient fill**: Horizontal linear gradient across full width: `#001B3D -> #003A7A -> #001B3D`.
2. **Bottom highlight**: 3px gradient bar at bottom, `rgba(74,144,217,0.5) -> rgba(74,144,217,0)`.
3. **Text**: White (#FFFFFF), font weight 900, Arial Black/Impact, fontSize = `h * 0.9`. Position = `(x + textX*w, y + textY*h)`.

**Drop behavior:** Always snaps to x=0, y=0.

**Text interaction:** Draggable horizontally only (textX updates, textY locked). Double-click to edit inline.

**Interactions:** Move, resize, text drag (horizontal), text edit (dblclick), Alt+drag clone, Delete.

---

### 7. MRE Logo (`type: 'image'`)

**Config (non-config-driven):**
```json
{ "x": 0, "y": 0, "w": 200, "h": 200, "src": "elements/mre.png" }
```

**Rendering:** `ctx.drawImage(img, x, y, w, h)` — preloaded via imageCache.

**Default size:** 200x200, aspect ratio adjusted from natural image dimensions on load.

**Note:** Image is the only type that does NOT use the config proxy pattern. Properties are stored directly on the shape object.

**Interactions:** Move, resize, Alt+drag clone, Delete.

---

### 8. Final (`type: 'final'`)

**Config:**
```json
{
  "x": 0, "y": 960, "w": 1920, "h": 120,
  "plc": "",
  "count_tag": "",
  "plan": "999",
  "actual": "999",
  "uptime": "100%",
  "performance": "100%"
}
```

**Rendering:**
1. **Navy gradient label bar**: Full-width across top `labelH = h*0.25`. Same gradient as header: `#001B3D -> #003A7A -> #001B3D`.
2. **Four equal boxes** (boxW = w/4), for each:
   a. White fill for value area (below label bar)
   b. Black border around full box, width = `max(3, sqrt(boxW*h)*0.03)`, lineJoin=miter
   c. **Label**: White text on navy, font weight 900, Arial Black/Impact, fontSize = `labelH*0.85`. Labels: "PLAN", "ACTUAL", "UPTIME", "PERFORMANCE".
   d. **Value**: Black text, font weight 900, Arial Black/Impact, fontSize = `(h-labelH)*0.85`.

**Drop behavior:** Always snaps to x=0, y=1080-h (bottom of canvas).

**Not movable/resizable:** Click selects but does not drag. hitHandle returns null for final type.

**Interactions:** Select, properties modal (dblclick, PLC + Count Tag), Delete.

---

### 9. Status (`type: 'status'`)

**Config:**
```json
{
  "x": 0, "y": 270, "w": 1920, "h": 540,
  "text": "STATUS"
}
```

**Rendering:**
1. **Background**: Black (#000000) at globalAlpha 0.2 (20% opacity).
2. **Text**: Black at 50% opacity (`rgba(0,0,0,0.5)`), font weight 900, Arial Black/Impact, fontSize = `h * 0.6`, centered.

**Drop behavior:** Always snaps to x=0, y=270 (center 50% of canvas).

**Always renders last** (Pass 5) — overlays everything including Final.

**Selection handles:** White (#fff) instead of dark, for visibility against dark overlay.

**Interactions:** Move, resize, text edit (dblclick), Alt+drag clone, Delete.

---

## Child Icon Types

All children are positioned at `(parentX + relX * parentH, parentY + relY * parentH)` with base size `parentH * 0.28`, scaled by `iconScale` (default 1).

### Icon children (used by both process and press)
- **`diamond`**: Filled diamond, radius = sz*0.4. Properties: `color`.
- **`gear`**: 8-tooth gear with white center hole, outer=sz*0.4, inner=sz*0.28. Properties: `color`.
- **`stopwatch`**: Stroked circle + top button + hand at ~2 o'clock. Properties: `color`.
- **`x`**: Two crossed lines, radius=sz*0.25, thick stroke (sz*0.22), lineCap=butt. Properties: `color`.
- **`rabbit`**: Rabbit silhouette (two ears, head circle, body ellipse), radius = sz*0.4. Properties: `color`.
- **`label`**: Text rendered in Arial Black/Impact, font weight 900, fontSize = sz * 1.0 * `scale`. Has draggable resize handles (left/right) to adjust scale. Properties: `color`, `text`, `scale`.
- **`wrench`**: Wrench silhouette rotated -45deg. Properties: `color`.
- **`pm`**: Bold "PM" text, fontSize = sz * 0.55, Arial Black/Impact weight 900. Properties: `color`.

### Structural children (used by press)
- **`preflight`**: Tall rectangle, 80% of parent height, width = 40% of its height. Has own background color and flash behavior. Border width = `max(2, sqrt(w*h)*0.025)`. Properties: `backgroundColor` (default #00FF00), `flashing` (default false).
- **`feeder`**: Rounded-corner rectangle above parent, 50% of parent height, 4:3 aspect ratio, corner radius = `min(w,h)*0.15`. Has own background color and flash behavior. Border width same formula as preflight. Properties: `backgroundColor` (default #00FF00), `flashing` (default false).

---

## Shared Rendering Constants

- `HANDLE_SIZE = 10` (canvas coords)
- `MIN_SIZE = 20` (canvas coords)
- `SNAP_ANGLE = PI/12` (15 degrees)
- Border width formula (shared by process, press, buffer, multibuffer): `max(4, sqrt(w*h) * 0.04)`
- Final border width: `max(3, sqrt(boxW*h) * 0.03)`
- Preflight/feeder border width: `max(2, sqrt(w*h) * 0.025)`

## Config Proxy Pattern

Process, press, buffer, multibuffer, header, final, and status all use `Object.defineProperty` to proxy `s.x/y/w/h` to `s.config.x/y/w/h`, so the generic drag/resize code modifies the config directly.

Arrow uses read-only computed getters for x/y/w/h (bounding box) since it uses x1/y1/x2/y2 endpoints instead.

Image type does NOT use the config proxy pattern — it stores x/y/w/h directly on the shape object.

## Font Stack Reference

- **System font** (process text, multibuffer text): `-apple-system, BlinkMacSystemFont, sans-serif`
- **Display font** (labels, header, status, final): `"Arial Black", "Impact", sans-serif` at weight 900

## Shape ID Scheme

All shape IDs and child IDs are UUIDs generated via `crypto.randomUUID()`. This provides stable identifiers for:
- SSE update keying (match incoming updates to shapes by UUID)
- Saving/loading layouts (IDs persist across serialization)
- Child interaction tracking (drag, edit, resize handles all key on child.id)
