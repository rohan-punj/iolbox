// D2 — Floating edges. Ports the xyflow "floating edges" recipe to
// @xyflow/svelte: each edge endpoint is the intersection of the centre-to-centre
// line with the neighbour-facing side of the node's rectangle, so cables exit
// the facing side and re-anchor live on drag.

import { Position, type InternalNode } from "@xyflow/svelte";

interface Point {
  x: number;
  y: number;
}

/**
 * Intersection of the line from `intersectionNode`'s centre to `targetNode`'s
 * centre with `intersectionNode`'s rectangle perimeter. Adapted from the xyflow
 * floating-edges example.
 */
function dims(node: InternalNode): { w: number; h: number } {
  return {
    w: node.measured.width ?? node.width ?? 64,
    h: node.measured.height ?? node.height ?? 64,
  };
}

function getNodeIntersection(intersectionNode: InternalNode, targetNode: InternalNode): Point {
  const iDim = dims(intersectionNode);
  const tDim = dims(targetNode);
  const iw = iDim.w / 2;
  const ih = iDim.h / 2;
  const ip = intersectionNode.internals.positionAbsolute;
  const tp = targetNode.internals.positionAbsolute;
  const tw = tDim.w / 2;
  const th = tDim.h / 2;

  const x2 = ip.x + iw; // intersection node centre
  const y2 = ip.y + ih;
  const x1 = tp.x + tw; // target node centre
  const y1 = tp.y + th;

  const xx1 = (x1 - x2) / (2 * iw) - (y1 - y2) / (2 * ih);
  const yy1 = (x1 - x2) / (2 * iw) + (y1 - y2) / (2 * ih);
  const a = 1 / (Math.abs(xx1) + Math.abs(yy1) || 1);
  const xx3 = a * xx1;
  const yy3 = a * yy1;
  const x = 2 * iw * (xx3 + yy3) * 0.5 + x2;
  const y = 2 * ih * (-xx3 + yy3) * 0.5 + y2;

  return { x, y };
}

/** Which side of `node` the point sits on → a Position for the bezier tangent. */
function getEdgePosition(node: InternalNode, point: Point): Position {
  const n = node.internals.positionAbsolute;
  const nx = Math.round(n.x);
  const ny = Math.round(n.y);
  const px = Math.round(point.x);
  const py = Math.round(point.y);
  const w = node.measured.width ?? node.width ?? 64;
  const h = node.measured.height ?? node.height ?? 64;

  if (px <= nx + 1) return Position.Left;
  if (px >= nx + w - 1) return Position.Right;
  if (py <= ny + 1) return Position.Top;
  if (py >= ny + h - 1) return Position.Bottom;
  return Position.Top;
}

export interface EdgeParams {
  sx: number;
  sy: number;
  tx: number;
  ty: number;
  sourcePos: Position;
  targetPos: Position;
}

/** Endpoints + tangent sides for a floating edge between two internal nodes. */
export function getEdgeParams(source: InternalNode, target: InternalNode): EdgeParams {
  const sourceIntersection = getNodeIntersection(source, target);
  const targetIntersection = getNodeIntersection(target, source);
  return {
    sx: sourceIntersection.x,
    sy: sourceIntersection.y,
    tx: targetIntersection.x,
    ty: targetIntersection.y,
    sourcePos: getEdgePosition(source, sourceIntersection),
    targetPos: getEdgePosition(target, targetIntersection),
  };
}
