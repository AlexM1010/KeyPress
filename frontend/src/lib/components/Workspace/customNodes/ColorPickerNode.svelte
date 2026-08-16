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
	 *
	 * `storeResultAs` is **optional in the type as well as on screen**, and that
	 * is the contract rather than a convenience: a name that was never given has
	 * to be absent from the payload, because the run state's "unset" is what the
	 * Branch node's `isSet` operator asks about. Writing `""` there would be
	 * writing down a name the user did not choose.
	 */
	type ColorPickerNodeData = {
		color: string;
		x: number;
		y: number;
		tolerance: number;
		timeoutMs: number;
		storeResultAs?: string;
	};

	export let id: string;
	export let title: string = 'Wait For Color';
	export let icon: ComponentType = Palette;
	export let color: string = 'bg-gradient-to-r from-indigo-400 to-indigo-500';
	export let highlightColor: string = 'bg-indigo-500';

	// `storeResultAs` is deliberately absent from here. Every other field is
	// backfilled into a payload that predates it, but backfilling this one with
	// `""` would put an empty name into every macro ever saved - and an empty name
	// is not the same thing as no name to the code that reads it back.
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

	/**
	 * Node connection point configuration.
	 *
	 * Two outputs: `right` is the match - the id every edge this app has ever
	 * saved carries, which is why the colour that *did* appear keeps the handle it
	 * always had - and `timeout` is where the token goes when it never did.
	 *
	 * The timeout output is conditional on the graph rather than on the payload.
	 * Draw an edge from it and a timeout becomes a handled branch (`task-success`,
	 * and the run carries on down that edge); leave it unwired and the node
	 * behaves exactly as it always has, reporting a timeout as the error it is.
	 * See `colorTimedOut` in `backend/actions_color.go`.
	 */
	const handles: HandleConfig[] = [
		{ id: 'right', type: 'source', position: Position.Right, offsetY: 35 },
		{ id: 'timeout', type: 'source', position: Position.Right, offsetY: 65 },
		{ id: 'left', type: 'target', position: Position.Left, offsetY: 50 }
	];

	// The "store result as" box is bound to a local of its own rather than
	// straight to `data.storeResultAs`, so that a cleared box can *remove* the
	// field instead of setting it to `""`. `bind:value` has no way to express
	// that, and the difference is the whole point of the field: a name that was
	// never stored is unset, which is what `isSet` distinguishes.
	let storeResultAs: string = data.storeResultAs ?? '';

	/**
	 * Copies the box into the payload, or takes the field back out when it is
	 * blank.
	 *
	 * Called from a reactive statement that names only `storeResultAs`, so this
	 * runs when the box changes and not when anything else in `data` does - the
	 * reads and writes of `data` are inside the function, where Svelte 4's
	 * dependency tracking does not look. That is what keeps a write here from
	 * feeding back into a re-run of itself.
	 *
	 * Whitespace is trimmed only for the emptiness test, not for what is stored:
	 * the backend trims a name on both sides of the exchange, so a name typed with
	 * a trailing space still finds its value, and echoing the user's own spelling
	 * back into the box is less startling than silently editing it.
	 */
	function syncStoreResultAs(name: string) {
		if (name.trim() === '') {
			if ('storeResultAs' in data) delete data.storeResultAs;
		} else if (data.storeResultAs !== name) {
			data.storeResultAs = name;
		}
	}

	$: syncStoreResultAs(storeResultAs);
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

		<!-- Remembering the colour that matched, for a later Branch node -->
		<div class="grid gap-1.5">
			<label class="color-label" for="store-result-{id}">Store result as</label>
			<input
				id="store-result-{id}"
				type="text"
				autocomplete="off"
				spellcheck="false"
				placeholder="Leave blank to store nothing"
				bind:value={storeResultAs}
				class="color-input"
			/>
			<p class="color-help">
				Stores the colour actually matched - six lower-case hex digits, no
				<code>#</code>, as in <code>00ff00</code>. A timeout stores nothing, so the name stays unset
				and a later Branch can ask <code>is set</code>.
			</p>
		</div>

		<!-- Which wire is which. The rows are in the same order as the two
		     output handles down the right-hand side. -->
		<ul class="color-outputs">
			<li class="color-output">
				<span class="color-output-name color-match">match</span>
				<span class="color-output-when">the colour appeared</span>
			</li>
			<li class="color-output">
				<span class="color-output-name color-timeout">timeout</span>
				<span class="color-output-when">it never did - unwired, that is an error</span>
			</li>
		</ul>
	</div>
</NodeWrapper>

<style>
	.color-label {
		font-size: 0.75rem;
		font-weight: 500;
		color: var(--main-text);
	}

	/* Themed like the shared number and select inputs rather than left to the
	   browser's default, since a node sits on a translucent card that is dark
	   under the dark theme. */
	.color-input {
		width: 100%;
		height: 2rem;
		padding: 0 0.5rem;
		border-radius: 0.375rem;
		border: 1px solid var(--border);
		background-color: var(--main);
		color: var(--main-text);
		font-size: 0.8rem;
		transition: background-color 0.3s;
	}

	.color-input:hover {
		background-color: var(--main-hover);
	}

	.color-input:focus {
		outline: none;
	}

	.color-help {
		font-size: 0.7rem;
		line-height: 1.1rem;
		color: var(--main-text);
		opacity: 0.75;
	}

	.color-help code {
		font-family: monospace;
	}

	.color-outputs {
		display: flex;
		flex-direction: column;
		gap: 0.25rem;
		margin: 0;
		padding: 0;
		list-style: none;
	}

	.color-output {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		padding: 0.25rem 0.5rem;
		border-radius: 0.375rem;
		background: var(--main);
	}

	.color-output-name {
		font-family: monospace;
		font-size: 0.7rem;
		font-weight: 600;
		padding: 0.1rem 0.4rem;
		border-radius: 0.25rem;
		color: var(--secondary-text);
	}

	.color-match {
		background: rgba(79, 70, 229, 0.9);
	}

	.color-timeout {
		background: rgba(217, 119, 6, 0.9);
	}

	.color-output-when {
		font-size: 0.7rem;
		opacity: 0.75;
	}
</style>
