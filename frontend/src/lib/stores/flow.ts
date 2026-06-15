// frontend/src/lib/stores/flow.ts
import { writable, type Writable } from 'svelte/store';
import type { Position, Node, Edge } from '@xyflow/svelte';

export type HandleConfig = {
    type: 'source' | 'target';
    position: Position;
    id?: string;
    offsetX?: number;
    offsetY?: number;
  };

export interface NodeData extends Node{
    id: string;
    type: string;
    data: Record<string, any>;
}

export interface MouseClickNodeData extends NodeData {
    data: {
        buttonType: 'left' | 'middle' | 'right';
        numberOfClicks: number;
        clickDelay: number;
        pressReleaseDelay: number;
        releaseAfterPress: boolean;
        scrollDirection: ('Vertical' | 'Horizontal')[];
        scrollLines: number;
    }
}

export interface DelayNodeData extends NodeData {
    data: {
        delayType: 'Fixed' | 'Random';
        time: number;
        minTime: number;
        maxTime: number;
    }
}

// Default nodes for new flows
const defaultNodes: NodeData[] = [
  {
    id: 'startnode-1',
    type: 'StartNode',
    position: { x: 100, y: 200 },
    data: { label: 'Start', icon: 'Play', color: 'bg-gradient-to-r from-blue-500 to-blue-600' }
  },
  {
    id: 'delaynode-1',
    type: 'DelayNode',
    position: { x: 400, y: 200 },
    data: { delayType: 'Fixed', time: 1000 }
  }
];

const defaultEdges: Edge[] = [
  { id: 'edge-1', source: 'startnode-1', target: 'delaynode-1', type: 'smoothstep' }
];

// Initialize writable stores with default data
export const nodesData: Writable<NodeData[]> = writable(defaultNodes);
export const edgesData: Writable<Edge[]> = writable(defaultEdges);
