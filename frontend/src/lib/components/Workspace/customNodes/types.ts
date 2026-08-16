// src/lib/components/customNodes/types.ts
import type { Position } from '@xyflow/svelte';

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
