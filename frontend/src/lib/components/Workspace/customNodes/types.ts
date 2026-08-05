// src/lib/components/customNodes/types.ts
import type { Position, Node } from '@xyflow/svelte';

export type HandleConfig = {
  type: 'source' | 'target';
  position: Position;
  id?: string;
  offsetX?: number;
  offsetY?: number;
};

export interface SVGNodeData extends Record<string, unknown> {
  label?: string;
  svgPath: string;
  width?: number;
  height?: number;
  handles?: HandleConfig[];
  styles?: {
    fill?: string;
    stroke?: string;
    strokeWidth?: number;
  };
}

export interface SVGNode extends Node {
  type: 'svg-node';
  dragHandle: string;
  data: SVGNodeData;
}

export function isSVGNode(node: Node): node is SVGNode {
  return node.type === 'svg-node';
}

export interface NodeStyle {
  fill?: string;
  stroke?: string;
  strokeWidth?: number;
}

export interface NodeShape {
  name: string;
  svgPath: string;
  defaultSize: { width: number; height: number };
  // Default handles for this shape
  defaultHandles: HandleConfig[];
}

export interface SVGNodeData {
  shape: NodeShape;
  label?: string;
  styles?: NodeStyle;
  // Optional override for default handles
  handles?: HandleConfig[];
  width?: number;
  height?: number;
}

export interface AutomationNode {
  id: string;
  type: 'startNode' | 'endNode' | 'clickNode' | 'moveNode' | 'keyNode' | 'delayNode';
  data: {
      label: string;
      clickType?: 'left' | 'right' | 'double';
      key?: string;
      delay?: number;
      mouseX?: number;
      mouseY?: number;
  };
  position: {
      x: number;
      y: number;
  };
}