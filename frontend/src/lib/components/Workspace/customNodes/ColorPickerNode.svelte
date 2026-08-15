<!-- ColorPickerNode.svelte -->
<script lang="ts">
	import { Position } from '@xyflow/svelte';
	import { Palette } from 'lucide-svelte';
	import type { ComponentType } from 'svelte';
	import NodeWrapper from './nodeComponents/NodeWrapper.svelte';
	import NumberInput from './nodeComponents/NumberInput.svelte';
	import TimeInput from './nodeComponents/TimeInput.svelte';
	import type { HandleConfig } from './types';

	/**
	 * Data payload of a `ColorPickerNode`.
	 *
	 * At runtime the node blocks until the pixel at (`x`, `y`) matches `color`
	 * to within `tolerance` on every RGB channel, giving up after `timeoutMs`.
	 * Declared here rather than in `$lib/stores/flow` to keep the payload next
	 * to the only component that owns it.
	 */
	type ColorPickerNodeData = {
		color: string;
		x: number;
		y: number;
		tolerance: number;
		timeoutMs: number;
	};

	export let id: string;
	export let title: string = 'Wait For Color';
	export let icon: ComponentType = Palette;
	export let color: string = 'bg-gradient-to-r from-indigo-400 to-indigo-500';
	export let highlightColor: string = 'bg-indigo-500';

	const DEFAULT_DATA: ColorPickerNodeData = {
		color: '#00ff00',
		x: 0,
		y: 0,
		tolerance: 10,
		timeoutMs: 30000
	};

	// Svelte Flow passes the node's data payload here, not the whole node - and it
	// is the *same object* the graph holds, so every edit below lands in the graph
	// by reference and is what gets saved. The controls therefore bind straight to
	// `data`; binding them to a local store (as this node used to) drops the
	// user's choices on save.
	//
	// That reference is the whole of the contract now. The graph's nodes are deep
	// `$state` (see `$lib/stores/flow.svelte`), so a `bind:value` writing
	// `data.color` in place is an ordinary tracked write and the edit *is* its own
	// notification - nothing has to be announced afterwards.
	export let data: ColorPickerNodeData = { ...DEFAULT_DATA };

	// Persisted nodes can predate newer fields, so backfill whatever is missing
	// without clobbering saved values. This has to mutate `data` in place:
	// reassigning it (`data = { ...DEFAULT_DATA, ...data }`) would detach this
	// component from the store's object and silently discard every later edit.
	Object.assign(data, { ...DEFAULT_DATA, ...data });

	// Node connection point configuration
	const handles: HandleConfig[] = [
		{ id: 'right', type: 'source', position: Position.Right, offsetY: 50 },
		{ id: 'left', type: 'target', position: Position.Left, offsetY: 50 }
	];

	// Svelte Flow's NodeWrapper passes a fixed prop set (selected, isConnectable,
	// positionAbsoluteX, ...) to every custom node. Referencing $$restProps silences
	// the "created with unknown prop" warnings for the ones we don't declare.
	$$restProps;
</script>

<NodeWrapper {id} {icon} {title} {color} type="ColorPickerNode" {handles}>
	<!-- `nodrag` keeps Svelte Flow from turning a click on a control into a node
         drag; it is matched against the event target's ancestors, so wrapping the
         shared inputs here covers them all. -->
	<div class="nodrag grid gap-4">
		<!-- Target Colour -->
		<div class="grid gap-2">
			<div class="flex items-center justify-between">
				<span class="text-sm font-medium --main-text">Target Color</span>
				<span class="font-mono text-sm">{data.color}</span>
			</div>
			<input
				type="color"
				aria-label="Target color"
				class="nodrag w-full h-12 cursor-pointer rounded-lg border border-gray-200 bg-white/50 transition-all duration-200 hover:border-indigo-300 focus:outline-none focus:ring-2 focus:ring-indigo-500"
				bind:value={data.color}
			/>
		</div>

		<!-- Screen Position -->
		<div class="flex items-center justify-between gap-2">
			<NumberInput label="X" bind:value={data.x} minValue={-100000} maxValue={100000} />
			<NumberInput label="Y" bind:value={data.y} minValue={-100000} maxValue={100000} />
		</div>

		<!-- Match Tolerance (per RGB channel, 0-255) -->
		<NumberInput label="Tolerance" bind:value={data.tolerance} minValue={0} maxValue={255} />

		<!-- Give-up Timeout -->
		<TimeInput
			label="Timeout"
			bind:value={data.timeoutMs}
			defaultValue={30}
			startingUnit="s"
			minValue={0}
			{highlightColor}
		/>
	</div>
</NodeWrapper>
