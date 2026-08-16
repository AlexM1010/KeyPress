<!-- SequenceNode.svelte -->
<script lang="ts">
	/**
	 * The node that expresses fan-out.
	 *
	 * Under the control-flow interpreter (`docs/control-flow-interpreter.md`) a
	 * run is one execution token walking the graph, and an execution output
	 * takes exactly one outgoing edge - the rule Unreal Blueprints uses. Dragging
	 * a second wire off one pin is therefore refused by the editor
	 * (`isValidConnection` in `Flow.svelte`) and by the backend at run start.
	 * "Do this, then that" is said with this node instead: the token leaves by
	 * `out-1` and walks that whole subtree, comes back, leaves by `out-2`, and so
	 * on.
	 *
	 * Written in runes, unlike the six older node components. New files go on the
	 * runes side of the migration; a file is wholly one dialect or the other, and
	 * this one needs `$derived` and `$effect` anyway - the handle list changes at
	 * run time, which nothing else in `customNodes/` does.
	 */
	import { ListOrdered, Minus, Plus } from 'lucide-svelte';
	import { Position, useUpdateNodeInternals } from '@xyflow/svelte';
	import { untrack, type ComponentType } from 'svelte';
	import NodeWrapper from './nodeComponents/NodeWrapper.svelte';
	import type { HandleConfig } from './types';
	import { detachOutputEdges } from '$lib/stores/flow.svelte';

	/** Data payload of a `SequenceNode`: how many outputs it currently offers. */
	type SequenceNodeData = {
		outputs: number;
	};

	type Props = {
		id: string;
		title?: string;
		icon?: ComponentType;
		color?: string;
		isConnectable?: boolean;
		data?: SequenceNodeData;
	};

	/**
	 * Fewest outputs a Sequence can have. One output is an ordinary node with a
	 * longer name, so the node only means anything from two up.
	 */
	const MIN_OUTPUTS = 2;

	/**
	 * Most outputs a Sequence can have, and the reason is the ordering, not the
	 * screen space.
	 *
	 * The interpreter takes a node's outgoing edges in **ascending string order
	 * of `sourceHandle`** - a plain bytewise sort, both in Go (`sort.Strings`)
	 * and in `nodeLabels.ts`. Under that sort `"out-10"` comes before `"out-2"`,
	 * so a tenth output would silently reorder the user's macro: the branch they
	 * added last would run second. There are two ways out - zero-pad the ids
	 * (`out-01`) so they sort as numbers do, or stop at nine - and this is the
	 * second.
	 *
	 * Capping is the kinder of the two. Padding buys a limit nobody reaches (a
	 * hundred outputs on one node is not a macro anyone can read) at the cost of
	 * a handle id that no longer matches the number printed beside it, and of a
	 * width that would itself have to be capped and would break every macro
	 * saved before it changed. Nine branches off one node is already past what
	 * fits down the side of a card, and a tenth branch is expressible today by
	 * hanging a second Sequence off the ninth output - which nests, reads better
	 * on the canvas, and needs no new rule.
	 */
	const MAX_OUTPUTS = 9;

	const DEFAULT_DATA: SequenceNodeData = {
		outputs: MIN_OUTPUTS
	};

	let {
		id,
		title = 'Sequence',
		icon = ListOrdered,
		color = 'bg-gradient-to-r from-blue-500 to-blue-600',
		isConnectable = true,
		data = { ...DEFAULT_DATA }
	}: Props = $props();

	// Svelte Flow passes the node's data payload here, not the whole node - and
	// it is the *same object* the graph holds, so every edit below lands in the
	// graph by reference and is what gets saved. The graph's nodes are deep
	// `$state`, so `data.outputs = 3` is an ordinary tracked write and the edit
	// *is* its own notification.
	//
	// Backfilled in place, like every other node: reassigning `data` would detach
	// this component from the store's object and silently discard later edits.
	//
	// Inside `untrack` because this file is runes and `data` is a reactive prop:
	// reading it bare at the top level warns (`state_referenced_locally`), and
	// the warning is right to be suspicious - a backfill that re-ran would put
	// the default back over the user's saved value. Running it once, off the
	// graph, is exactly what the legacy nodes' `Object.assign` does and why they
	// do it at init rather than in a reactive statement.
	untrack(() => Object.assign(data, { ...DEFAULT_DATA, ...data }));

	/** How many outputs to draw, whatever the payload happens to say. */
	const outputs = $derived(clamp(data.outputs));

	function clamp(count: number): number {
		if (!Number.isFinite(count)) return MIN_OUTPUTS;
		return Math.min(MAX_OUTPUTS, Math.max(MIN_OUTPUTS, Math.trunc(count)));
	}

	/**
	 * The handle id of the nth output, counting from 1.
	 *
	 * This string is the save format and the interpreter's ordering key at once -
	 * it is written into every edge's `sourceHandle` - so it is built in exactly
	 * one place. See `MAX_OUTPUTS` for why it is never zero-padded.
	 */
	const outputHandleId = (index: number): string => `out-${index}`;

	/**
	 * One target handle on the left and `outputs` source handles spread evenly
	 * down the right-hand side.
	 *
	 * `offsetY` is a percentage of the node's height (`getHandlePosition` in
	 * `NodeWrapper.svelte` turns it into `top: N%`), so the spread survives the
	 * card being expanded and collapsed. It deliberately does not try to line
	 * each handle up with its row in the list below - that would need the node
	 * measured, and the list is numbered in the same order the handles are, which
	 * is the part that has to be readable.
	 */
	const handles: HandleConfig[] = $derived([
		{ id: 'left', type: 'target', position: Position.Left, offsetY: 50 },
		...Array.from({ length: outputs }, (_, index) => ({
			id: outputHandleId(index + 1),
			type: 'source' as const,
			position: Position.Right,
			offsetY: ((index + 1) / (outputs + 1)) * 100
		}))
	]);

	// Svelte Flow measures a node's handles once and caches them; adding or
	// removing one at run time has to be announced, or the new handle is drawn
	// but invisible to the connection logic (and the removed one goes on being
	// connectable). This is the whole reason the hook exists, and the design
	// document calls it out as the frontend half of a multi-output node.
	const updateNodeInternals = useUpdateNodeInternals();

	/**
	 * Announces this node's handles to Svelte Flow.
	 *
	 * `count` is declared and deliberately unused: the hook takes only a node id,
	 * and naming the count as an argument is what gives the `$effect` below a
	 * tracked read to depend on. Without it the effect would run once and never
	 * again, which is the bug this whole call exists to avoid.
	 */
	const announceHandles = (nodeId: string, _count: number): void => updateNodeInternals(nodeId);

	// An `$effect` rather than a line in each click handler, because the update
	// has to happen *after* the DOM has the new handle in it: effects run
	// post-render, and the hook then defers to the next frame itself.
	$effect(() => {
		announceHandles(id, outputs);
	});

	function addOutput() {
		if (outputs >= MAX_OUTPUTS) return;
		data.outputs = outputs + 1;
	}

	/**
	 * Drops the last output.
	 *
	 * The last one specifically, rather than a chosen one, because the ids are
	 * positional: removing the middle output of three would either leave a gap
	 * (`out-1`, `out-3`, which still runs correctly but numbers the node's own
	 * rows misleadingly) or renumber the survivors - and renumbering rewrites
	 * the `sourceHandle` of edges the user drew, which is a silent reordering of
	 * their macro. Removing from the end cannot do either.
	 *
	 * The edge attached to the handle goes with it. An edge whose `sourceHandle`
	 * names a handle that no longer exists is drawn from nowhere and is followed
	 * by nothing, so leaving it behind would be leaving the graph in a state the
	 * user cannot see and cannot fix.
	 */
	function removeOutput() {
		if (outputs <= MIN_OUTPUTS) return;
		detachOutputEdges(id, outputHandleId(outputs));
		data.outputs = outputs - 1;
	}
</script>

<NodeWrapper {id} {icon} {title} {color} type="Sequence" {handles} {isConnectable}>
	<div class="space-y-4">
		<p class="sequence-help">
			Each output runs in turn, top to bottom. One finishes completely before the next starts.
		</p>

		<ol class="sequence-outputs">
			{#each Array.from({ length: outputs }, (_, index) => index + 1) as position (position)}
				<li class="sequence-output">
					<span class="sequence-output-number">{position}</span>
					<span class="sequence-output-handle">{outputHandleId(position)}</span>
				</li>
			{/each}
		</ol>

		<div class="sequence-controls">
			<button
				class="sequence-button"
				onclick={removeOutput}
				disabled={outputs <= MIN_OUTPUTS}
				aria-label="Remove output"
				title={outputs <= MIN_OUTPUTS
					? `A Sequence needs at least ${MIN_OUTPUTS} outputs`
					: 'Remove the last output'}
			>
				<Minus class="w-4 h-4" />
			</button>
			<span class="sequence-count">{outputs} outputs</span>
			<button
				class="sequence-button"
				onclick={addOutput}
				disabled={outputs >= MAX_OUTPUTS}
				aria-label="Add output"
				title={outputs >= MAX_OUTPUTS
					? `A Sequence holds at most ${MAX_OUTPUTS} outputs - chain another Sequence for more`
					: 'Add an output'}
			>
				<Plus class="w-4 h-4" />
			</button>
		</div>
	</div>
</NodeWrapper>

<style>
	.sequence-help {
		font-size: 0.75rem;
		line-height: 1.25rem;
		color: var(--main-text);
		opacity: 0.75;
	}

	.sequence-outputs {
		display: flex;
		flex-direction: column;
		gap: 0.25rem;
		margin: 0;
		padding: 0;
		list-style: none;
	}

	.sequence-output {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		padding: 0.25rem 0.5rem;
		border-radius: 0.375rem;
		background: var(--main);
	}

	.sequence-output-number {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 1.25rem;
		height: 1.25rem;
		border-radius: 50%;
		background: var(--accent);
		color: var(--secondary-text);
		font-size: 0.7rem;
		font-weight: 600;
	}

	.sequence-output-handle {
		font-family: monospace;
		font-size: 0.75rem;
		opacity: 0.7;
	}

	.sequence-controls {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.5rem;
	}

	.sequence-count {
		font-size: 0.8rem;
	}

	.sequence-button {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 1.75rem;
		height: 1.75rem;
		border-radius: 0.375rem;
		background: var(--main);
		transition: background-color 0.2s;
	}

	.sequence-button:hover:not(:disabled) {
		background: var(--main-hover);
	}

	.sequence-button:disabled {
		opacity: 0.4;
		cursor: not-allowed;
	}
</style>
