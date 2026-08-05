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

/**
 * The `data` payload carried by a node.
 *
 * Svelte Flow hands this - and *only* this - to a custom node component's `data`
 * prop; the surrounding `id` / `type` / `position` stay on the node itself and are
 * passed as separate props. Node-level and payload-level shapes are therefore
 * distinct types and must not be used interchangeably.
 */
export type NodeDataPayload = Record<string, unknown>;

/** A whole node in the flow graph: identity, position and its data payload. */
export type FlowNode<TData extends NodeDataPayload = NodeDataPayload> = Node<TData> & {
    id: string;
    type: string;
    data: TData;
};

/** Data payload of a `MouseClickNode`. */
export type MouseClickNodeData = {
    buttonType: 'left' | 'middle' | 'right';
    numberOfClicks: number;
    clickDelay: number;
    pressReleaseDelay: number;
    releaseAfterPress: boolean;
    scrollDirection: ('Vertical' | 'Horizontal')[];
    scrollLines: number;
};

/** Data payload of a `DelayNode`. */
export type DelayNodeData = {
    delayType: 'Fixed' | 'Random';
    time: number;
    minTime: number;
    maxTime: number;
};

/**
 * Data payload of a `StartNode`.
 *
 * `macroKeys` is the recorded trigger hotkey, in the order the keys went down.
 * The presentational fields are optional because they only exist on nodes that
 * were seeded from `defaultNodes` below; the component takes its own icon,
 * title and colour from its props.
 */
export type StartNodeData = {
    macroKeys: string[];
    label?: string;
    icon?: string;
    color?: string;
};

// `KeyPressNode`'s payload is declared in the component that owns it
// (`customNodes/KeyPressNode.svelte`), the way `ColorPickerNode` declares its
// own. Nothing outside that component reads it.

// Default nodes for new flows
const defaultNodes: FlowNode[] = [
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

/**
 * The one edge a new flow starts with, shaped exactly like an edge the user
 * draws by dragging between two handles.
 *
 * Two details matter and are easy to get wrong:
 *
 * - No `type`. Svelte Flow merges `defaultEdgeOptions` *under* each edge
 *   (`{ ...defaults, ...edge }`), so any type named here wins over the
 *   `customedge` that `Flow.svelte` configures and the edge silently renders
 *   as a built-in instead - no delete button, and none of `CustomEdge`'s
 *   styling. Leaving it unset is what routes this edge through the same path
 *   as every other edge in the app.
 * - Explicit handle ids. Each node declares its handles with ids (`right` on
 *   the source side, `left` on the target side); naming them here keeps the
 *   edge unambiguous and means it survives a save/load round trip in the same
 *   shape a user-drawn edge does.
 */
const defaultEdges: Edge[] = [
  {
    id: 'edge-1',
    source: 'startnode-1',
    sourceHandle: 'right',
    target: 'delaynode-1',
    targetHandle: 'left'
  }
];

// Initialize writable stores with default data
export const nodesData: Writable<FlowNode[]> = writable(defaultNodes);
export const edgesData: Writable<Edge[]> = writable(defaultEdges);
