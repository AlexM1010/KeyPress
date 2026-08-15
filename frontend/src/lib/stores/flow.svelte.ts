// frontend/src/lib/stores/flow.svelte.ts
import { writable, type Writable } from 'svelte/store';
import type { Node, Edge } from '@xyflow/svelte';

// `HandleConfig` used to be declared here as well as in
// `customNodes/types.ts`, character for character, and node components imported
// it from whichever of the two they happened to reach for first - four from one,
// three from the other. Two declarations of one type is one that can drift, so
// this re-exports the node-side declaration rather than repeating it: a handle
// describes a node's connection points, which is the custom nodes' business
// rather than the graph store's.
export type { HandleConfig } from '$lib/components/Workspace/customNodes/types';

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

/**
 * The graph the workspace has on its canvas.
 *
 * A class with `$state` fields rather than the two `writable`s this used to be,
 * because Svelte Flow 1.x takes `nodes` and `edges` as plain arrays it binds to
 * (`bind:nodes`), not as stores it subscribes to. An object is what lets a
 * module export reassignable state at all: `export let nodes = $state([])` gives
 * importers a copy of the binding rather than the binding, so the array has to
 * live behind a property that both sides reach through the same reference.
 *
 * The `$state` is deliberately *deep*, which is the whole reason this migration
 * simplifies rather than merely moves things. Svelte Flow's own docs reach for
 * `$state.raw` for the performance of not proxying large graphs, and that would
 * be the wrong trade here: every node component edits its payload in place
 * (`data.time = 2500`), and a raw array does not notice a write that deep. Deep
 * state does, so the edit is reactive by construction and the workspace's
 * unsaved-changes check re-runs off it directly.
 *
 * That retires `markGraphEdited`, which used to be the other half of an
 * in-place write: with stores, mutating `data` changed no reference and so
 * announced nothing, and every node component ended its script with a reactive
 * block calling it. Nothing needs announcing now - the write *is* the
 * notification. It also retires the reason those blocks had to be declared last
 * in each component, and the `<svelte:options immutable />` in Svelte Flow's own
 * wrapper that kept the resulting identity-update from recursing forever. A
 * graph this app's size costs nothing to proxy, and correctness that falls out
 * of the model beats correctness maintained by a rule in seven files.
 *
 * A node's payload is a `$state` proxy once it is in here, which matters in one
 * place: `structuredClone` throws on a proxy, so anything copying a payload out
 * (duplicating a node, handing one to the backend) has to take a
 * `$state.snapshot` first.
 */
class FlowGraph {
	nodes = $state<FlowNode[]>(defaultNodes);
	edges = $state<Edge[]>(defaultEdges);
}

export const graph = new FlowGraph();

/**
 * Identity of the macro the workspace currently has open: the display name the
 * user typed, and the id (bare filename) of the file it lives in - "" when this
 * graph has never been saved.
 *
 * These were plain `let`s inside `Flow.svelte`, and had to move out here the
 * moment something other than the workspace could decide which macro is open.
 * The macro list opens one by writing its graph into the state above and
 * navigating; the name and id have to travel with the graph, or the workspace
 * would show a macro it cannot save back over its own file - the backend would
 * see an unknown id and refuse the save as a name someone else already owns.
 *
 * Still `writable`s, unlike the graph above. Nothing about the Svelte Flow
 * upgrade touches them, they are read as `$macroName` in a handful of places,
 * and converting them would be churn dressed up as migration.
 */
export const macroName: Writable<string> = writable('');
export const macroID: Writable<string> = writable('');

/**
 * Whether the macro on screen differs from the one on disk.
 *
 * Driven by the workspace, which is the only place a graph can be edited: it
 * re-answers the question whenever the graph state changes - including an edit
 * made inside a node's `data` payload, which is now an ordinary tracked write
 * rather than something that had to announce itself. Read by anything that is
 * about to cost the user those edits - the macro list before it opens something
 * else over them. It keeps its last value after the workspace unmounts, which is
 * correct: nothing outside the workspace edits the graph, so the answer cannot
 * go stale while the user is elsewhere.
 */
export const isMacroDirty: Writable<boolean> = writable(false);

/**
 * The macro exactly as it was last written to (or read from) disk, serialised -
 * `null` when no baseline has been taken yet.
 *
 * A module-level value rather than component state, because the workspace is a
 * route: a trip to the macro list and back remounts it, and a baseline held in
 * the component would be retaken on the way back and quietly declare the user's
 * unsaved edits saved.
 */
let savedSnapshot: string | null = null;

export const getSavedSnapshot = (): string | null => savedSnapshot;

/**
 * Adopts `snapshot` as the state on disk, which makes the macro clean until the
 * next edit. Called after a save, and once the graph a load put on the canvas
 * has settled - see `serializeMacro`.
 */
export function markMacroSaved(snapshot: string): void {
	savedSnapshot = snapshot;
	isMacroDirty.set(false);
}

/** The subset of a node that reaches the file. */
type PersistableNode = {
	id: string;
	type?: string;
	position: { x: number; y: number };
	data?: unknown;
};

/** The subset of an edge that reaches the file. */
type PersistableEdge = {
	id: string;
	source: string;
	target: string;
	sourceHandle?: string | null;
	targetHandle?: string | null;
	type?: string;
};

/**
 * The macro as a string that changes exactly when the saved file would.
 *
 * Comparing two of these is how the workspace knows whether there is anything
 * to save. Being told the graph changed cannot answer that question by itself:
 * typing a delay back to the value it was saved at is a real edit that must
 * still leave the macro clean.
 *
 * Only the fields the Go `FlowData` keeps are included, and that is the point
 * rather than an economy: Svelte Flow also tracks its own bookkeeping on a node
 * (selection, drag state), none of which is saved, so including any of it would
 * have clicking a node report unsaved changes. Nodes and edges are sorted by id
 * for the same reason - the array order is Svelte Flow's business, not the
 * user's.
 */
export function serializeMacro(
	name: string,
	nodes: PersistableNode[],
	edges: PersistableEdge[]
): string {
	const byId = (a: { id: string }, b: { id: string }) => a.id.localeCompare(b.id);

	return JSON.stringify({
		name: name.trim(),
		nodes: nodes
			.slice()
			.sort(byId)
			.map((node) => ({
				id: node.id,
				type: node.type ?? '',
				position: { x: node.position.x, y: node.position.y },
				data: node.data ?? {}
			})),
		edges: edges
			.slice()
			.sort(byId)
			.map((edge) => ({
				id: edge.id,
				source: edge.source,
				target: edge.target,
				sourceHandle: edge.sourceHandle ?? '',
				targetHandle: edge.targetHandle ?? '',
				type: edge.type ?? ''
			}))
	});
}

/**
 * A saved macro in the shape the Go bindings hand it back.
 *
 * Declared structurally rather than as `backend.FlowData` so this module - which
 * every part of the flow imports - does not pull in the generated Wails
 * bindings. `position` is a loose number map on the Go model, hence the
 * optional coordinates.
 */
export type StoredNode = {
	id: string;
	type: string;
	data?: Record<string, unknown> | null;
	position?: { x?: number; y?: number } | null;
};

// `null` as well as absent: Wails v3 types a Go slice as `T[] | null`, because
// that is what an empty or omitted JSON array actually arrives as. v2's
// generated classes hid that behind a constructor; the plain interfaces v3
// generates do not, and a macro saved with no edges really does come back with
// `edges: null`.
export type StoredFlowData = {
	id?: string;
	name?: string;
	nodes?: StoredNode[] | null;
	edges?: Edge[] | null;
};

/**
 * Narrows saved nodes back to the `{ x, y }` position shape Svelte Flow
 * expects. The Go model types a position as a loose number map, so this is what
 * turns a file off disk into a graph.
 */
function toFlowNodes(nodes: StoredNode[] | undefined | null): FlowNode[] {
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
 * Whether the workspace state holds a graph that was put there deliberately -
 * either read back off disk or opened from the macro list - as opposed to the
 * `defaultNodes` this module starts with.
 *
 * A module-level flag rather than reactive state, because nothing renders from
 * it: it exists so `Flow.svelte` reads the last opened macro exactly once. The
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
 * anything else on screen. Custom node components edit their `data` payload in
 * place, so any other holder of these objects would end up editing the user's
 * live graph through them.
 *
 * An empty graph deliberately leaves the canvas alone while still adopting the
 * name and id: a macro the user had deleted every node from would otherwise
 * come back nameless, and saving it again would be refused as a name someone
 * else owns.
 *
 * The macro comes in clean, but its baseline is dropped rather than taken here:
 * the node components have not mounted yet, and they backfill their newer
 * fields into `data` as they do, so a snapshot taken now would be of a graph
 * that no longer exists a frame later. The workspace takes it once the canvas
 * has settled - see `captureSavedSnapshot` in Flow.svelte.
 */
export function openMacroInWorkspace(data: StoredFlowData): void {
	macroName.set(data.name ?? '');
	macroID.set(data.id ?? '');

	if (data.nodes && data.nodes.length > 0) {
		graph.nodes = toFlowNodes(data.nodes);
		graph.edges = data.edges ?? [];
	}

	savedSnapshot = null;
	isMacroDirty.set(false);
	markWorkspaceHydrated();
}
