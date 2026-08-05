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

/**
 * Identity of the macro the workspace currently has open: the display name the
 * user typed, and the id (bare filename) of the file it lives in - "" when this
 * graph has never been saved.
 *
 * These were plain `let`s inside `Flow.svelte`, and had to move out here the
 * moment something other than the workspace could decide which macro is open.
 * The macro list opens one by writing its graph into the stores above and
 * navigating; the name and id have to travel with the graph, or the workspace
 * would show a macro it cannot save back over its own file - the backend would
 * see an unknown id and refuse the save as a name someone else already owns.
 */
export const macroName: Writable<string> = writable('');
export const macroID: Writable<string> = writable('');

/**
 * A saved macro in the shape the Go bindings hand it back.
 *
 * Declared structurally rather than as `main.FlowData` so this module - which
 * every part of the flow imports - does not pull in the generated Wails
 * bindings. `position` is a loose number map on the Go model, hence the
 * optional coordinates.
 */
export type StoredNode = {
	id: string;
	type: string;
	data?: Record<string, unknown>;
	position?: { x?: number; y?: number };
};

export type StoredFlowData = {
	id?: string;
	name?: string;
	nodes?: StoredNode[];
	edges?: Edge[];
};

/**
 * Narrows saved nodes back to the `{ x, y }` position shape Svelte Flow
 * expects. The Go model types a position as a loose number map, so this is what
 * turns a file off disk into a graph.
 */
function toFlowNodes(nodes: StoredNode[] | undefined): FlowNode[] {
	return (nodes ?? []).map((node) => ({
		id: node.id,
		type: node.type,
		data: node.data ?? {},
		// Saved edges carry their own handle ids and are used exactly as they
		// come off disk, so nothing equivalent is needed for them.
		position: { x: node.position?.x ?? 0, y: node.position?.y ?? 0 }
	}));
}

/**
 * Whether the workspace stores hold a graph that was put there deliberately -
 * either read back off disk or opened from the macro list - as opposed to the
 * `defaultNodes` this module starts with.
 *
 * A module-level flag rather than a store, because nothing renders from it: it
 * exists so `Flow.svelte` reads the last opened macro exactly once. The
 * workspace is a route, so navigating to the macro list and back remounts it,
 * and a second `LoadLastFile` on the way back would throw away both the macro
 * the user just picked and any unsaved edits they had made.
 */
let workspaceHydrated = false;

export const isWorkspaceHydrated = (): boolean => workspaceHydrated;

export const markWorkspaceHydrated = (): void => {
	workspaceHydrated = true;
};

/**
 * Puts a saved macro into the workspace: its graph, its name and its id.
 *
 * The caller must pass a freshly parsed `FlowData` and not share it with
 * anything else on screen. Custom node components mutate their `data` payload
 * in place, so any other holder of these objects would end up editing the
 * user's live graph through them.
 *
 * An empty graph deliberately leaves the canvas alone while still adopting the
 * name and id: a macro the user had deleted every node from would otherwise
 * come back nameless, and saving it again would be refused as a name someone
 * else owns.
 */
export function openMacroInWorkspace(data: StoredFlowData): void {
	macroName.set(data.name ?? '');
	macroID.set(data.id ?? '');

	if (data.nodes && data.nodes.length > 0) {
		nodesData.set(toFlowNodes(data.nodes));
		edgesData.set(data.edges ?? []);
	}

	markWorkspaceHydrated();
}
