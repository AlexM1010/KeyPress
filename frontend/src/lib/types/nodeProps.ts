// src/lib/types/nodeProps.ts
import type { Node } from '@xyflow/svelte';
import type { BlockData } from './block'; // Adjust the path as necessary
import type { TVariant } from './tailwind'; // Adjust the path as necessary

export interface BaseNodeProps {
  id: string;
  type: string;
  data: BlockData;
  position: { x: number; y: number };
  selected: boolean;
  width?: string;
  height: number;
  variant?: TVariant;
  showLabel?: boolean;
  className?: string;
  labelPosition?: 'left' | 'right' | 'center';
  // Include any additional props that @xyflow/svelte expects
  sourcePosition?: string; // e.g., 'left', 'right'
  targetPosition?: string; // e.g., 'left', 'right'
  dragHandle?: string;
  selectable?: boolean;
  deletable?: boolean;
  draggable?: boolean;
  parentId?: string;
  // ... add other props as needed
}
