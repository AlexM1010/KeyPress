// src/lib/components/customNodes/types.ts
import type { Position } from '@xyflow/svelte';

export type HandleConfig = {
	type: 'source' | 'target';
	position: Position;
	id?: string;
	offsetX?: number;
	offsetY?: number;
};
