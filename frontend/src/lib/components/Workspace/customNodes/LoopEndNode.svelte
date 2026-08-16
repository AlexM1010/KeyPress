<!-- LoopEndNode.svelte -->
<script lang="ts">
	/**
	 * The bottom of a loop, and it does nothing at all.
	 *
	 * That is the whole node, and it is the Sequence node's argument again: it has
	 * one output, `back`, the editor has already wired that output to the partner
	 * Loop Start, and `executeLoopEndTask` returns "" - "the only output" - so the
	 * token goes back round. There is no jump, no partner lookup in the walk, and
	 * nothing in `backend/interpreter.go` that knows this node exists.
	 *
	 * **The user never draws that edge and cannot usefully redraw it.** This half
	 * is created with its Loop Start, deleted with it, and duplicated with it - see
	 * `createLoopPair`, `withLoopPartners` and `repairLoopPairs` in
	 * `$lib/stores/flow.svelte`, which are the three places that keep the pair
	 * whole.
	 *
	 * **Nothing here writes a payload.** The backend reads none from this node, and
	 * the `loopId` the store puts on both halves is the editor's own bookkeeping -
	 * so this component deliberately has no defaults to backfill and no controls to
	 * offer. The card exists to be a place on the canvas to end the body at, and to
	 * say what happens next.
	 *
	 * Written in runes, like its Loop Start.
	 */
	import { Position } from '@xyflow/svelte';
	import { CornerUpLeft } from 'lucide-svelte';
	import type { ComponentType } from 'svelte';
	import NodeWrapper from './nodeComponents/NodeWrapper.svelte';
	import type { HandleConfig } from './types';
	import { LOOP_BACK_HANDLE } from '$lib/stores/flow.svelte';

	/**
	 * Data payload of a `LoopEndNode`: the editor's pairing id, and nothing else.
	 * Written by the store when the pair is made, read by the store when the pair
	 * has to be found again, and ignored by Go.
	 */
	type LoopEndNodeData = {
		loopId?: string;
	};

	type Props = {
		id: string;
		title?: string;
		icon?: ComponentType;
		color?: string;
		isConnectable?: boolean;
		data?: LoopEndNodeData;
	};

	let {
		id,
		title = 'Loop End',
		icon = CornerUpLeft,
		color = 'bg-gradient-to-r from-teal-500 to-teal-600',
		isConnectable = true
	}: Props = $props();

	/**
	 * One way in, one way out, and the way out is the edge the editor drew.
	 *
	 * `back` is the save format: it is the `sourceHandle` of the edge
	 * `createLoopPair` wires to the partner Loop Start, so the id comes from the
	 * store that writes that edge rather than being spelled a second time here.
	 */
	const handles: HandleConfig[] = [
		{ id: 'left', type: 'target', position: Position.Left, offsetY: 50 },
		{ id: LOOP_BACK_HANDLE, type: 'source', position: Position.Right, offsetY: 50 }
	];
</script>

<NodeWrapper {id} {icon} {title} {color} type="Loop End" {handles} {isConnectable}>
	<div class="grid gap-3">
		<p class="loop-end-help">
			The end of one turn of the loop. Wire the last node of the body into this one; the run then
			goes back to the Loop Start, which decides whether to go round again.
		</p>

		<ul class="loop-end-outputs">
			<li class="loop-end-output">
				<span class="loop-end-output-name">{LOOP_BACK_HANDLE}</span>
				<span class="loop-end-output-when">back to this loop's start - wired for you</span>
			</li>
		</ul>
	</div>
</NodeWrapper>

<style>
	.loop-end-help {
		font-size: 0.75rem;
		line-height: 1.25rem;
		color: var(--main-text);
		opacity: 0.75;
	}

	.loop-end-outputs {
		display: flex;
		flex-direction: column;
		gap: 0.25rem;
		margin: 0;
		padding: 0;
		list-style: none;
	}

	.loop-end-output {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		padding: 0.25rem 0.5rem;
		border-radius: 0.375rem;
		background: var(--main);
	}

	.loop-end-output-name {
		font-family: monospace;
		font-size: 0.7rem;
		font-weight: 600;
		padding: 0.1rem 0.4rem;
		border-radius: 0.25rem;
		background: rgba(13, 148, 136, 0.9);
		color: var(--secondary-text);
	}

	.loop-end-output-when {
		font-size: 0.7rem;
		opacity: 0.75;
	}
</style>
