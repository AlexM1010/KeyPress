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

/** How far a duplicate is offset from the node it was copied from, in px. */
const DUPLICATE_OFFSET = 40;

// A loop, and the pair of nodes it is made of
// -------------------------------------------
// A loop is two nodes and an edge between them, and **the user never draws that
// edge** - the editor does. Creating a Loop Start creates its Loop End and wires
// the end's `back` output to the start; deleting either half deletes both.
// `backend/actions_loop.go` states the case for it: with a user-drawn back edge
// there is no syntactic enclosing loop, so "which loop does Break exit" has no
// answer and overlapping cycles have no innermost one.
//
// That makes the pair an *editor* construct, and it lives in this module for the
// same reason `duplicateNodes` does - it is a change to the graph, and there are
// three ways to ask for one (the palette, a delete, a duplicate) which all have
// to make exactly the same shape.
//
// **The backend ignores `loopId` entirely.** Frames are keyed by the Loop Start's
// node id and the walk follows an ordinary edge on an ordinary handle, so nothing
// in Go reads a partner id or checks that a pair is intact - see the comment on
// `executeLoopEndTask`. It exists so that the three operations above can find the
// other half. Everything here therefore degrades rather than throwing: a half
// whose partner is missing is simply a half.

/** The `type` a saved Loop Start carries. Matches `loopStartNodeType` in Go. */
export const LOOP_START_NODE_TYPE = 'LoopStartNode';

/** The `type` a saved Loop End carries. Matches `loopEndNodeType` in Go. */
export const LOOP_END_NODE_TYPE = 'LoopEndNode';

/**
 * The pair's handle ids, which are the save format: they are written into every
 * loop edge's `sourceHandle`, and `executeLoopStartTask` returns two of these
 * three strings to say which way the token leaves. Declared here rather than in
 * the two components because this module draws the `back` edge, and an id spelled
 * differently in the edge and in the handle is a wire attached to nothing.
 */
export const LOOP_BODY_HANDLE = 'body';
export const LOOP_DONE_HANDLE = 'done';
export const LOOP_BACK_HANDLE = 'back';

/**
 * The input handle both halves carry, and the one the `back` edge lands on.
 * Every node in the app names its target handle `left`; the loop is not special.
 */
const LOOP_TARGET_HANDLE = 'left';

/**
 * How far apart the two halves of a new pair are placed, in px. A node is 300px
 * wide (`NodeWrapper`), so this leaves a gap the body can be dropped into rather
 * than putting the Loop End on top of its own start.
 */
const LOOP_PARTNER_GAP = 420;

/** Whether this node is one half of a loop. */
const isLoopHalf = (node: FlowNode | undefined): boolean =>
	node?.type === LOOP_START_NODE_TYPE || node?.type === LOOP_END_NODE_TYPE;

/** The pairing id a loop half carries, if it carries a usable one. */
function loopIdOf(node: FlowNode | undefined): string | undefined {
	const loopId = node?.data?.loopId;
	return typeof loopId === 'string' && loopId !== '' ? loopId : undefined;
}

const nodeById = (id: string): FlowNode | undefined => graph.nodes.find((node) => node.id === id);

/** Writes the pairing id onto a half. `data` is a payload map, so this is a plain
 *  property write - and a tracked one once the node is in the graph. */
function setLoopId(node: FlowNode, loopId: string): void {
	node.data.loopId = loopId;
}

/**
 * The other half of `nodeId`'s loop, or `undefined` if it has none.
 *
 * Answered twice over, and the order matters. The `loopId` is the intended
 * answer: it survives the two halves being dragged apart, rewired, or having
 * their back edge deleted by hand, which the edge cannot. The back edge is the
 * fallback, for a macro saved before this pairing existed or edited outside the
 * app - there the wiring is the only statement of the pair there is, and reading
 * it means such a file can still be edited as a pair rather than silently coming
 * apart the first time the user deletes one end.
 */
export function loopPartnerId(nodeId: string): string | undefined {
	const node = nodeById(nodeId);
	if (!isLoopHalf(node) || node === undefined) return undefined;

	const isStart = node.type === LOOP_START_NODE_TYPE;
	const wanted = isStart ? LOOP_END_NODE_TYPE : LOOP_START_NODE_TYPE;

	const loopId = loopIdOf(node);
	if (loopId !== undefined) {
		const partner = graph.nodes.find(
			(other) => other.type === wanted && other.id !== nodeId && loopIdOf(other) === loopId
		);
		if (partner) return partner.id;
	}

	// `?? ''` on the handle for the same reason `serializeMacro` writes it: an
	// unused handle is `null` on a Svelte Flow edge and `''` once saved.
	const back = graph.edges.find((edge) =>
		isStart
			? edge.target === nodeId &&
				(edge.sourceHandle ?? '') === LOOP_BACK_HANDLE &&
				nodeById(edge.source)?.type === LOOP_END_NODE_TYPE
			: edge.source === nodeId && (edge.sourceHandle ?? '') === LOOP_BACK_HANDLE
	);
	if (!back) return undefined;

	const partnerId = isStart ? back.source : back.target;
	return nodeById(partnerId)?.type === wanted ? partnerId : undefined;
}

/**
 * The edge that makes a loop a loop: the Loop End's `back` output, wired to its
 * Loop Start.
 *
 * No `type` field, deliberately - `defaultEdgeOptions` in `Flow.svelte` is merged
 * *under* each edge, so naming a type here would win over `customedge` and the
 * edge would render as a built-in with no delete button. Same reasoning as
 * `defaultEdges` above.
 */
const backEdgeBetween = (endId: string, startId: string): Edge => ({
	id: crypto.randomUUID(),
	source: endId,
	sourceHandle: LOOP_BACK_HANDLE,
	target: startId,
	targetHandle: LOOP_TARGET_HANDLE
});

/** The half a lone half needs, minted fresh beside it. */
function mintPartnerFor(half: FlowNode, loopId: string): FlowNode {
	const isStart = half.type === LOOP_START_NODE_TYPE;
	return {
		id: crypto.randomUUID(),
		type: isStart ? LOOP_END_NODE_TYPE : LOOP_START_NODE_TYPE,
		position: {
			x: half.position.x + (isStart ? LOOP_PARTNER_GAP : -LOOP_PARTNER_GAP),
			y: half.position.y
		},
		// Only the pairing id. A Loop End has no payload of its own at all, and a
		// minted Loop Start is left at the defaults its component backfills - which
		// is a loop with no count and no condition, i.e. one that runs until the
		// user stops it. Copying the count off the partner the user did *not* select
		// would be inventing a configuration they never asked for.
		data: { loopId }
	};
}

/**
 * Puts a whole loop on the canvas: a Loop Start, its Loop End, and the `back`
 * edge between them. Called by the drop handler in `Flow.svelte` - the palette
 * offers one "Loop" entry, not two halves, because half a loop is not a thing a
 * user can usefully be given.
 */
export function createLoopPair(position: { x: number; y: number }): {
	start: FlowNode;
	end: FlowNode;
	edge: Edge;
} {
	const loopId = crypto.randomUUID();
	const start: FlowNode = {
		id: crypto.randomUUID(),
		type: LOOP_START_NODE_TYPE,
		position: { ...position },
		// Empty but for the pairing id, like every other node the palette drops:
		// `LoopStartNode` backfills its own defaults into whatever it is handed.
		data: { loopId }
	};
	const end = mintPartnerFor(start, loopId);
	const edge = backEdgeBetween(end.id, start.id);

	graph.nodes = [...graph.nodes, start, end];
	graph.edges = [...graph.edges, edge];

	return { start, end, edge };
}

/**
 * Widens a pending deletion to whole loops: the other half of every pair being
 * deleted, and every edge attached to what that adds.
 *
 * Wired up as `onbeforedelete` on `<SvelteFlow>`, which is the one place every
 * route to a deletion passes through - the Backspace key, the hover menu's bin,
 * and the selection toolbar all reach `deleteElements`, and that calls this hook
 * before it filters the arrays. Doing it here rather than in the three call sites
 * is what makes "you cannot delete half a loop" a property of the editor instead
 * of a habit of its buttons.
 *
 * The edges matter as much as the node. Svelte Flow has already collected the
 * edges attached to the nodes *it* was asked to delete; the partner this adds
 * brings its own - the body's edge into a Loop End, most obviously - and an edge
 * whose endpoint has gone is wiring the user can neither see nor remove.
 *
 * Ids in, objects out, because the two sides want different things: the hook is
 * handed Svelte Flow's own node and edge objects and this only needs their
 * identity, while what it returns has to be the graph's own objects.
 */
export function withLoopPartners(
	nodeIds: string[],
	edgeIds: string[]
): { nodes: FlowNode[]; edges: Edge[] } {
	// Plain Sets: local to the call, read by nothing reactive.
	/* eslint-disable svelte/prefer-svelte-reactivity */
	const doomedNodes = new Set(nodeIds);
	const doomedEdges = new Set(edgeIds);
	/* eslint-enable svelte/prefer-svelte-reactivity */

	for (const id of [...doomedNodes]) {
		if (!isLoopHalf(nodeById(id))) continue;
		const partner = loopPartnerId(id);
		// A half whose partner is already going, or has none left, needs nothing.
		if (partner !== undefined) doomedNodes.add(partner);
	}

	for (const edge of graph.edges) {
		if (doomedNodes.has(edge.source) || doomedNodes.has(edge.target)) doomedEdges.add(edge.id);
	}

	return {
		nodes: graph.nodes.filter((node) => doomedNodes.has(node.id)),
		edges: graph.edges.filter((edge) => doomedEdges.has(edge.id))
	};
}

/**
 * Re-pairs the loop halves among a set of freshly made copies, and reports the
 * nodes and edges that has to add.
 *
 * This is the trap in duplicating a loop. The copies carry their originals'
 * `loopId`, so left alone the canvas would hold two Loop Starts and two Loop Ends
 * all claiming to be the same loop - and `loopPartnerId` would answer with
 * whichever it found first, so deleting one pair could take a node out of the
 * other. Every copied pair therefore gets a **new** `loopId` and a **new** back
 * edge: `duplicateNodes` deliberately copies no edges between duplicated nodes,
 * and the back edge is the one edge the editor owns rather than the user, so it
 * is re-created here rather than left absent.
 *
 * **Half a pair is promoted to a whole one**, rather than the duplication being
 * refused. Two reasons. Refusing would have Ctrl+D on a Loop Start do nothing
 * visible - or, worse, copy the rest of a mixed selection and silently drop the
 * loop from it - and a control that sometimes does nothing is the harder thing to
 * learn. And promoting is what every other entry point already does: the palette
 * hands out pairs, a delete removes pairs, so a duplicate yielding a pair is the
 * same rule a third time. The invariant "a loop half always has a partner" is
 * what the whole design rests on, and it is cheaper to maintain here than to
 * defend everywhere it could be broken.
 */
function repairLoopPairs(
	originals: FlowNode[],
	copies: FlowNode[]
): { nodes: FlowNode[]; edges: Edge[] } {
	const minted: FlowNode[] = [];
	const edges: Edge[] = [];

	// eslint-disable-next-line svelte/prefer-svelte-reactivity
	const copyOfOriginal = new Map<string, FlowNode>();
	originals.forEach((original, index) => copyOfOriginal.set(original.id, copies[index]));

	// eslint-disable-next-line svelte/prefer-svelte-reactivity
	const paired = new Set<string>();

	originals.forEach((original, index) => {
		const copy = copies[index];
		if (!isLoopHalf(copy) || paired.has(copy.id)) return;

		const loopId = crypto.randomUUID();
		setLoopId(copy, loopId);
		paired.add(copy.id);

		const partnerId = loopPartnerId(original.id);
		const partnerCopy = partnerId === undefined ? undefined : copyOfOriginal.get(partnerId);

		// `paired.has` guards a hand-edited graph in which two starts name one end:
		// the second start is given an end of its own rather than a second back edge
		// into a node that already has one.
		let mate: FlowNode;
		if (partnerCopy !== undefined && !paired.has(partnerCopy.id)) {
			setLoopId(partnerCopy, loopId);
			paired.add(partnerCopy.id);
			mate = partnerCopy;
		} else {
			mate = mintPartnerFor(copy, loopId);
			// Selected like the copies, so dragging the result apart moves the whole
			// loop and not the half the user happened to have picked.
			mate.selected = true;
			minted.push(mate);
		}

		const [start, end] = copy.type === LOOP_START_NODE_TYPE ? [copy, mate] : [mate, copy];
		edges.push(backEdgeBetween(end.id, start.id));
	});

	return { nodes: minted, edges };
}

/**
 * Copies the given nodes onto the canvas, and returns the copies.
 *
 * Lives here rather than in the component that used to own it because there is
 * now more than one way to ask for it - the hover menu on a single node, and the
 * selection toolbar on several - and every part of the shape below has to be
 * identical whichever asked. The old comment on this code warned that the id
 * scheme "has to agree" with the one `onDrop` uses; two callers of two copies of
 * this would have been a third and a fourth thing to keep in agreement.
 *
 * Three details are load-bearing:
 *
 * - `data` is deep-copied, via `$state.snapshot`. Every node component edits the
 *   payload it is handed in place - that is how an edit reaches the graph - so a
 *   shallow copy would leave the clone editing the original's nested arrays, and
 *   ticking a modifier on the copy would tick it on the node it came from.
 *   `structuredClone` cannot do this job any more: `graph.nodes` is deep
 *   `$state`, so `node.data` is a proxy and structuredClone throws
 *   `DataCloneError` on one. The snapshot unwraps and deep-copies in a single
 *   step.
 * - A fresh `crypto.randomUUID()`, the same id scheme `onDrop` in Flow.svelte
 *   uses: once it is on the canvas a duplicate has to be indistinguishable from
 *   any other node, and a reused id would have Svelte Flow key two nodes alike.
 * - The copies come out *selected* and the originals do not. Duplicating several
 *   nodes and then dragging them apart is the whole point of doing it to a
 *   selection, and that only works if the selection follows the copies. It also
 *   means duplicating twice in a row gives two separate pairs rather than three
 *   copies of the first node.
 *
 * Edges between the duplicated nodes are deliberately not copied. A duplicate
 * lands offset on top of its original and the user is about to move it; guessing
 * that they also want the wiring re-created, in a graph where an edge carries
 * handle ids and execution order, is a bigger assumption than this is worth.
 *
 * The one exception is a loop's back edge, which is the editor's rather than the
 * user's: a copied pair is re-paired and re-wired, and a copied half is given a
 * partner. See `repairLoopPairs`, which is also where the returned array can come
 * back longer than the ids it was given.
 */
export function duplicateNodes(ids: string[]): FlowNode[] {
	// A lookup local to this call, gone by the time it returns - nothing reads it
	// reactively, so the plain Set is right and SvelteSet would only add a proxy.
	// eslint-disable-next-line svelte/prefer-svelte-reactivity
	const wanted = new Set(ids);
	// The originals are kept alongside their copies, rather than only mapped over,
	// because re-pairing a loop has to look its partner up by the id the *original*
	// had - the copy carries a fresh one.
	const originals = graph.nodes.filter((node) => wanted.has(node.id));
	const copies = originals.map((node) => ({
		...node,
		id: crypto.randomUUID(),
		position: {
			x: node.position.x + DUPLICATE_OFFSET,
			y: node.position.y + DUPLICATE_OFFSET
		},
		data: $state.snapshot(node.data),
		selected: true
	})) as FlowNode[];

	if (copies.length === 0) return [];

	const { nodes: minted, edges: backEdges } = repairLoopPairs(originals, copies);

	graph.nodes = [
		...graph.nodes.map((node) => (wanted.has(node.id) ? { ...node, selected: false } : node)),
		...copies,
		...minted
	];
	if (backEdges.length > 0) graph.edges = [...graph.edges, ...backEdges];

	return [...copies, ...minted];
}

/**
 * Whether an edge may still be drawn out of this node's output handle.
 *
 * A run is one execution token walking the graph
 * (`docs/control-flow-interpreter.md`), and it leaves a node by exactly one
 * edge per output - Unreal Blueprints' rule. A second wire off one pin has no
 * meaning under that model, so the editor refuses it while the user is still
 * dragging (`isValidConnection` in `Flow.svelte`) rather than letting the
 * backend reject the macro at run start.
 *
 * Only *outputs* are limited. Any number of edges may point into a node,
 * because arriving twice is arriving twice and the token simply runs it again.
 * That asymmetry is the whole model: it leaves a join drawable while making an
 * ambiguous fan-out undrawable, and fan-out is said with a Sequence node, whose
 * outputs are separate handles and so take one edge apiece.
 *
 * Here rather than in the component because it is the same question
 * `detachOutputEdges` below answers in reverse, over the same array - and
 * because a rule the canvas enforces is worth being able to test without
 * standing the canvas up.
 */
export function isOutputHandleFree(
	source: string | null | undefined,
	sourceHandle: string | null | undefined
): boolean {
	// `?? ''` on both sides for the same reason `serializeMacro` writes it: an
	// unused handle is `null` on a Svelte Flow edge and `''` once saved, and the
	// two have to compare equal.
	const handle = sourceHandle ?? '';
	return !graph.edges.some(
		(edge) => edge.source === source && (edge.sourceHandle ?? '') === handle
	);
}

/**
 * Drops every edge that leaves `nodeId` by `handleId`.
 *
 * For a node whose handles change at run time - `SequenceNode`, which the user
 * can take an output off - because an edge whose `sourceHandle` names a handle
 * that no longer exists is drawn from nowhere and followed by nothing: the
 * interpreter takes a node's outgoing edges by handle id, so such an edge is
 * dead wiring the user can neither see nor delete.
 *
 * Here rather than in the component for the same reason `duplicateNodes` is:
 * the graph is this module's, and removing a handle and removing its edge are
 * one change to it. Writing `graph.edges` is how a change reaches the canvas -
 * `Flow.svelte` binds this very array to `<SvelteFlow>`.
 */
export function detachOutputEdges(nodeId: string, handleId: string): void {
	graph.edges = graph.edges.filter(
		// `?? ''` for the same reason `serializeMacro` writes it: an unused handle
		// is `null` on a Svelte Flow edge and `''` once it has been saved.
		(edge) => !(edge.source === nodeId && (edge.sourceHandle ?? '') === handleId)
	);
}

/** The nodes Svelte Flow currently has selected, in graph order. */
export function selectedNodes(): FlowNode[] {
	return graph.nodes.filter((node) => node.selected);
}

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
