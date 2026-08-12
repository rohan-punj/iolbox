import { getSmoothStepPath, type Position } from "@xyflow/svelte";

export interface LinkGeometry {
  /** The cable's own path, in flow coordinates. */
  path: string;
  /** Point at curve parameter t in [0, 1]. */
  at(t: number): { x: number; y: number };
  /** A path parallel to path, offset by d along the local normal. */
  offsetPath(d: number, reversed: boolean): string;
  /** Chip anchors and their transform origins. */
  sChip: { x: number; y: number };
  tChip: { x: number; y: number };
  watcherChip: { x: number; y: number };
  sOrigin: "left" | "right";
  tOrigin: "left" | "right";
}

export interface RouteInput {
  sx: number;
  sy: number;
  tx: number;
  ty: number;
  sourcePos: Position;
  targetPos: Position;
  parallelSign: number;
  parallelCount: number;
  parallelIndex: number;
  px: number;
  py: number;
}

const PARALLEL_SPACING = 26;
const GRID_GAP = 20;
export const LANE_SPACING = GRID_GAP;
export const STEP_OFFSET = GRID_GAP;
export const CORNER_R = 4;

export function freeGeometry(input: RouteInput): LinkGeometry {
  const { sx, sy, tx, ty } = input;
  const off = input.parallelSign * PARALLEL_SPACING;
  const cx = (sx + tx) / 2 + input.px * off * 2;
  const cy = (sy + ty) / 2 + input.py * off * 2;

  const path = `M ${sx} ${sy} Q ${cx} ${cy} ${tx} ${ty}`;

  // Evaluate the quadratic B(u) so each chip follows its end of the curve.
  const at = (u: number) => {
    const m = 1 - u;
    return {
      x: m * m * sx + 2 * m * u * cx + u * u * tx,
      y: m * m * sy + 2 * m * u * cy + u * u * ty,
    };
  };

  const count = input.parallelCount;
  const single = count <= 1;
  const idx = input.parallelIndex;
  const tShift = single ? 0 : (idx - (count - 1) / 2) * 0.03;
  const su = 0.22 + tShift;
  const tu = 0.78 + tShift;
  const sPt = at(su);
  const tPt = at(tu);
  const wPt = at(0.5);

  const offsetPath = (d: number, reversed: boolean) => {
    const osx = sx + input.px * d, osy = sy + input.py * d;
    const ocx = cx + input.px * d, ocy = cy + input.py * d;
    const otx = tx + input.px * d, oty = ty + input.py * d;
    return reversed
      ? `M ${otx} ${oty} Q ${ocx} ${ocy} ${osx} ${osy}`
      : `M ${osx} ${osy} Q ${ocx} ${ocy} ${otx} ${oty}`;
  };

  const dx = tx - sx;
  return {
    path,
    at,
    offsetPath,
    sChip: { x: sPt.x, y: sPt.y },
    tChip: { x: tPt.x, y: tPt.y },
    watcherChip: { x: wPt.x, y: wPt.y },
    sOrigin: dx >= 0 ? "left" : "right",
    tOrigin: dx >= 0 ? "right" : "left",
  };
}

type Point = { x: number; y: number };
type Seg =
  | { kind: "line"; a: Point; b: Point }
  | { kind: "quad"; a: Point; c: Point; b: Point };

function samePoint(a: Point, b: Point): boolean {
  return a.x === b.x && a.y === b.y;
}

function distance(a: Point, b: Point): number {
  return Math.hypot(b.x - a.x, b.y - a.y);
}

function pointToward(from: Point, to: Point, length: number): Point {
  const total = distance(from, to);
  if (total === 0) return { ...from };
  const scale = length / total;
  return {
    x: from.x + (to.x - from.x) * scale,
    y: from.y + (to.y - from.y) * scale,
  };
}

function parsePathPoints(path: string): Point[] {
  const values = path.match(/-?(?:\d+\.?\d*|\.\d+)(?:e[-+]?\d+)?/gi)?.map(Number) ?? [];
  const points: Point[] = [];
  for (let i = 0; i + 1 < values.length; i += 2) {
    points.push({ x: values[i], y: values[i + 1] });
  }
  return points;
}

function dedupePoints(points: Point[]): Point[] {
  return points.filter((point, index) => index === 0 || !samePoint(point, points[index - 1]));
}

function makeSegments(points: Point[]): Seg[] {
  const corners = new Map<number, { entry: Point; control: Point; exit: Point }>();
  for (let i = 1; i < points.length - 1; i += 1) {
    const before = points[i - 1];
    const vertex = points[i];
    const after = points[i + 1];
    if (
      (before.x === vertex.x && vertex.x === after.x) ||
      (before.y === vertex.y && vertex.y === after.y)
    ) {
      continue;
    }
    const incoming = distance(before, vertex);
    const outgoing = distance(vertex, after);
    const radius = Math.min(CORNER_R, incoming / 2, outgoing / 2);
    if (radius <= 0) continue;
    corners.set(i, {
      entry: pointToward(vertex, before, radius),
      control: { ...vertex },
      exit: pointToward(vertex, after, radius),
    });
  }

  const segments: Seg[] = [];
  let cursor = points[0];
  for (let i = 1; i < points.length; i += 1) {
    const corner = corners.get(i);
    const lineEnd = corner?.entry ?? points[i];
    if (!samePoint(cursor, lineEnd)) segments.push({ kind: "line", a: cursor, b: lineEnd });
    if (corner) {
      segments.push({ kind: "quad", a: corner.entry, c: corner.control, b: corner.exit });
      cursor = corner.exit;
    } else {
      cursor = points[i];
    }
  }
  return segments;
}

function segmentLength(segment: Seg): number {
  if (segment.kind === "line") return distance(segment.a, segment.b);
  return (
    distance(segment.a, segment.b) +
    distance(segment.a, segment.c) +
    distance(segment.c, segment.b)
  ) / 2;
}

function segmentPoint(segment: Seg, t: number): Point {
  if (segment.kind === "line") {
    return {
      x: segment.a.x + (segment.b.x - segment.a.x) * t,
      y: segment.a.y + (segment.b.y - segment.a.y) * t,
    };
  }
  const m = 1 - t;
  return {
    x: m * m * segment.a.x + 2 * m * t * segment.c.x + t * t * segment.b.x,
    y: m * m * segment.a.y + 2 * m * t * segment.c.y + t * t * segment.b.y,
  };
}

function normal(a: Point, b: Point, amount: number): Point {
  const length = distance(a, b) || 1;
  return {
    x: (-(b.y - a.y) / length) * amount,
    y: ((b.x - a.x) / length) * amount,
  };
}

function plus(a: Point, b: Point): Point {
  return { x: a.x + b.x, y: a.y + b.y };
}

function offsetSegments(segments: Seg[], amount: number): Seg[] {
  return segments.map((segment) => {
    if (segment.kind === "line") {
      const n = normal(segment.a, segment.b, amount);
      return { kind: "line", a: plus(segment.a, n), b: plus(segment.b, n) };
    }
    const inNormal = normal(segment.a, segment.c, amount);
    const outNormal = normal(segment.c, segment.b, amount);
    return {
      kind: "quad",
      a: plus(segment.a, inNormal),
      c: plus(segment.c, plus(inNormal, outNormal)),
      b: plus(segment.b, outNormal),
    };
  });
}

function reverseSegment(segment: Seg): Seg {
  if (segment.kind === "line") return { kind: "line", a: segment.b, b: segment.a };
  return { kind: "quad", a: segment.b, c: segment.c, b: segment.a };
}

function emitPath(segments: Seg[], reversed = false): string {
  const ordered = reversed
    ? segments.slice().reverse().map(reverseSegment)
    : segments;
  const first = ordered[0].a;
  let path = `M ${first.x} ${first.y}`;
  for (const segment of ordered) {
    path += segment.kind === "line"
      ? ` L ${segment.b.x} ${segment.b.y}`
      : ` Q ${segment.c.x} ${segment.c.y} ${segment.b.x} ${segment.b.y}`;
  }
  return path;
}

function quantize(value: number): number {
  return Math.round(value / GRID_GAP) * GRID_GAP;
}

function direction(segment: Seg): Point {
  return { x: segment.b.x - segment.a.x, y: segment.b.y - segment.a.y };
}

export function structuredGeometry(input: RouteInput): LinkGeometry {
  const horizontal = Math.abs(input.tx - input.sx) >= Math.abs(input.ty - input.sy);
  const centerX = horizontal
    ? quantize((input.sx + input.tx) / 2 + input.parallelSign * LANE_SPACING)
    : undefined;
  const centerY = horizontal
    ? undefined
    : quantize((input.sy + input.ty) / 2 + input.parallelSign * LANE_SPACING);
  const [canonicalPath] = getSmoothStepPath({
    sourceX: input.sx,
    sourceY: input.sy,
    sourcePosition: input.sourcePos,
    targetX: input.tx,
    targetY: input.ty,
    targetPosition: input.targetPos,
    borderRadius: 0,
    offset: STEP_OFFSET,
    centerX,
    centerY,
  });
  // The installed helper emits zero-length Q tokens for radius 0; extracting
  // every coordinate pair and deduplicating them recovers its canonical corners.
  const parsed = dedupePoints(parsePathPoints(canonicalPath));
  const pts = parsed.length >= 2
    ? parsed
    : [{ x: input.sx, y: input.sy }, { x: input.tx, y: input.ty }];
  const builtSegments = makeSegments(pts);
  const segments: Seg[] = builtSegments.length > 0
    ? builtSegments
    : [{ kind: "line", a: pts[0], b: pts[pts.length - 1] }];
  const lengths = segments.map(segmentLength);
  const total = lengths.reduce((sum, length) => sum + length, 0) || 1;

  const at = (t: number): Point => {
    const target = Math.max(0, Math.min(1, t)) * total;
    let consumed = 0;
    for (let i = 0; i < segments.length; i += 1) {
      const length = lengths[i];
      if (target <= consumed + length || i === segments.length - 1) {
        const local = length === 0 ? 0 : (target - consumed) / length;
        return segmentPoint(segments[i], Math.max(0, Math.min(1, local)));
      }
      consumed += length;
    }
    return segmentPoint(segments[segments.length - 1], 1);
  };

  const count = input.parallelCount;
  const single = count <= 1;
  const idx = input.parallelIndex;
  const tShift = single ? 0 : (idx - (count - 1) / 2) * 0.03;
  const sPt = at(0.22 + tShift);
  const tPt = at(0.78 + tShift);
  const wPt = at(0.5);
  const firstDirection = direction(segments[0]);
  const lastDirection = direction(segments[segments.length - 1]);

  return {
    path: emitPath(segments),
    at,
    offsetPath: (amount: number, reversed: boolean) =>
      emitPath(offsetSegments(segments, amount), reversed),
    sChip: { x: sPt.x, y: sPt.y },
    tChip: { x: tPt.x, y: tPt.y },
    watcherChip: { x: wPt.x, y: wPt.y },
    sOrigin: firstDirection.x >= 0 ? "left" : "right",
    tOrigin: lastDirection.x >= 0 ? "right" : "left",
  };
}
