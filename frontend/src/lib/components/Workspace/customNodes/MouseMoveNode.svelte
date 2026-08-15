<!-- MouseMoveNode.svelte -->
<script lang="ts">
	import { Position } from '@xyflow/svelte';
	import type { ComponentType } from 'svelte';
	import { Mouse, ChevronDown } from 'lucide-svelte';

	// Component Imports
	import NodeWrapper from './nodeComponents/NodeWrapper.svelte';
	import Checkbox from './nodeComponents/Checkbox.svelte';
	import NumberInput from './nodeComponents/NumberInput.svelte';
	import Slider from './nodeComponents/Slider.svelte';
	import TimeInput from './nodeComponents/TimeInput.svelte';
	import ButtonGroup from './nodeComponents/ButtonGroup.svelte';
	import ButtonGroupItem from './nodeComponents/ButtonGroupItem.svelte';
	import type { HandleConfig } from './types';

	// Type definitions
	type PositionType = 'Mouse' | 'Fixed';
	type PathType = 'Straight' | 'Human';
	type SpeedType = 'Instant' | 'Human';

	type Coordinates = {
		x: number;
		y: number;
	};

	// The node's data payload - this is what Svelte Flow hands to the `data` prop.
	type MouseMoveTaskData = {
		startPosition: {
			type: PositionType;
			coordinates: Coordinates;
		};
		endPosition: {
			type: PositionType;
			coordinates: Coordinates;
		};
		dragWhileMoving: boolean;
		speed: {
			type: SpeedType;
			value: number;
			randomize: boolean;
			variance: number;
		};
		pathType: PathType;
		customPath: Coordinates[];
	};

	// Props.
	// `data` is the very object the graph holds for this node, so the edits below
	// (and the backfill further down) mutate it in place and are what gets saved.
	// Never reassign `data` - that detaches the component from the graph's object
	// and silently drops every edit.
	//
	// That reference is the whole of the contract now. The graph's nodes are deep
	// `$state` (see `$lib/stores/flow.svelte`), so an in-place write is an ordinary
	// tracked write and the edit *is* its own notification - the deep ones
	// (`data.startPosition.coordinates.x`) included, which is what deep state buys
	// over the raw kind Svelte Flow's own docs reach for.
	export let id: string;
	export let title: string = 'Mouse Move';
	export let icon: ComponentType = Mouse;
	export let color: string = 'bg-gradient-to-r from-green-500 to-green-600';
	export let highlightColor: string = 'bg-green-500';

	// Data initialization with default values
	export let data: MouseMoveTaskData = {
		startPosition: {
			type: 'Mouse',
			coordinates: { x: 0, y: 0 }
		},
		endPosition: {
			type: 'Fixed',
			coordinates: { x: 0, y: 0 }
		},
		dragWhileMoving: false,
		speed: {
			type: 'Instant',
			value: 500,
			randomize: false,
			variance: 20
		},
		pathType: 'Straight',
		customPath: []
	};

	/**
	 * Fill in whatever a persisted node is missing, to the leaf.
	 *
	 * This used to check only the top level, which is not where a payload is
	 * actually incomplete: a node saved with a `speed` object from before
	 * `variance` existed has a `speed`, so the old check passed and left
	 * `speed.variance` undefined - and the Go handler read that field straight
	 * out of the payload. Same for a `startPosition` whose `coordinates` predate
	 * a rename. The backend refuses such a node with a named reason now (see
	 * `readMouseMoveSettings`), which is the right behaviour for a payload it
	 * cannot complete - but it should not be seeing one at all, because the node
	 * that owns the shape is here and can simply fill it in.
	 *
	 * Every assignment is still guarded on the field being absent, and that is
	 * load-bearing rather than tidy: this runs reactively, and the graph's nodes
	 * are deep `$state`, so an unguarded write would invalidate `data`, re-run
	 * this, and write again - forever. Writing only what is missing is what lets
	 * it settle after one pass. It also means a saved value is never overwritten,
	 * which is the same promise the other nodes' one-shot `Object.assign` makes.
	 */
	$: {
		if (data.startPosition == null)
			data.startPosition = { type: 'Mouse', coordinates: { x: 0, y: 0 } };
		if (data.startPosition.type == null) data.startPosition.type = 'Mouse';
		if (data.startPosition.coordinates == null) data.startPosition.coordinates = { x: 0, y: 0 };
		if (data.startPosition.coordinates.x == null) data.startPosition.coordinates.x = 0;
		if (data.startPosition.coordinates.y == null) data.startPosition.coordinates.y = 0;

		if (data.endPosition == null) data.endPosition = { type: 'Fixed', coordinates: { x: 0, y: 0 } };
		if (data.endPosition.type == null) data.endPosition.type = 'Fixed';
		if (data.endPosition.coordinates == null) data.endPosition.coordinates = { x: 0, y: 0 };
		if (data.endPosition.coordinates.x == null) data.endPosition.coordinates.x = 0;
		if (data.endPosition.coordinates.y == null) data.endPosition.coordinates.y = 0;

		if (data.dragWhileMoving == null) data.dragWhileMoving = false;

		if (data.speed == null)
			data.speed = { type: 'Instant', value: 500, randomize: false, variance: 20 };
		if (data.speed.type == null) data.speed.type = 'Instant';
		if (data.speed.value == null) data.speed.value = 500;
		if (data.speed.randomize == null) data.speed.randomize = false;
		if (data.speed.variance == null) data.speed.variance = 20;

		if (data.pathType == null) data.pathType = 'Straight';
		if (data.customPath == null) data.customPath = [];
	}

	// Local UI state
	let showMovementSettings = false;

	/**
	 * Node connection handle configuration
	 */
	const handles: HandleConfig[] = [
		{ id: 'right', type: 'source', position: Position.Right, offsetY: 50 },
		{ id: 'left', type: 'target', position: Position.Left, offsetY: 50 }
	];

	// Available options
	const PATH_TYPES: PathType[] = ['Straight', 'Human'];
	const SPEED_TYPES: SpeedType[] = ['Instant', 'Human'];
	const POSITION_TYPES: PositionType[] = ['Fixed', 'Mouse'];

	// Constants
	const CONFIG = {
		POSITION: {
			MIN: -10000,
			MAX: 10000
		},
		SPEED: {
			DEFAULT: 500,
			MIN: 100,
			MAX: 5000
		},
		VARIANCE: {
			DEFAULT: 20,
			MIN: 0,
			MAX: 100
		}
	} as const;

	// Event handlers
	// Svelte Flow's NodeWrapper passes a fixed prop set (selected, isConnectable,
	// positionAbsoluteX, ...) to every custom node. Referencing $$restProps silences
	// the "created with unknown prop" warnings for the ones we don't declare.
	$$restProps;
</script>

<NodeWrapper {id} {icon} {title} {color} type="Move" {handles}>
	<div class="grid gap-6">
		<!-- Start Position Configuration -->
		<div class="grid gap-4">
			<h3 class="text-sm font-medium --main-text">Start Position</h3>
			<ButtonGroup variant="default">
				<ButtonGroupItem
					value="Fixed"
					on:click={() => (data.startPosition.type = 'Fixed')}
					active={data.startPosition.type === 'Fixed'}
					itemHighlightColor={highlightColor}
				>
					Fixed
				</ButtonGroupItem>
				<ButtonGroupItem
					value="Mouse"
					on:click={() => (data.startPosition.type = 'Mouse')}
					active={data.startPosition.type === 'Mouse'}
					disabled={data.endPosition.type === 'Mouse'}
					itemHighlightColor={highlightColor}
				>
					Mouse
				</ButtonGroupItem>
			</ButtonGroup>

			{#if data.startPosition.type === 'Fixed'}
				<div class="grid grid-cols-2 gap-4">
					<NumberInput
						label="X"
						bind:value={data.startPosition.coordinates.x}
						minValue={CONFIG.POSITION.MIN}
						maxValue={CONFIG.POSITION.MAX}
					/>
					<NumberInput
						label="Y"
						bind:value={data.startPosition.coordinates.y}
						minValue={CONFIG.POSITION.MIN}
						maxValue={CONFIG.POSITION.MAX}
					/>
				</div>
			{/if}
		</div>

		<!-- End Position Configuration -->
		<div class="grid gap-4">
			<h3 class="text-sm font-medium --main-text">End Position</h3>
			<ButtonGroup variant="default">
				<ButtonGroupItem
					value="Fixed"
					on:click={() => (data.endPosition.type = 'Fixed')}
					active={data.endPosition.type === 'Fixed'}
					itemHighlightColor={highlightColor}
				>
					Fixed
				</ButtonGroupItem>
				<ButtonGroupItem
					value="Mouse"
					on:click={() => (data.endPosition.type = 'Mouse')}
					active={data.endPosition.type === 'Mouse'}
					disabled={data.startPosition.type === 'Mouse'}
					itemHighlightColor={highlightColor}
				>
					Mouse
				</ButtonGroupItem>
			</ButtonGroup>

			{#if data.endPosition.type === 'Fixed'}
				<div class="grid grid-cols-2 gap-4">
					<NumberInput
						label="X"
						bind:value={data.endPosition.coordinates.x}
						minValue={CONFIG.POSITION.MIN}
						maxValue={CONFIG.POSITION.MAX}
					/>
					<NumberInput
						label="Y"
						bind:value={data.endPosition.coordinates.y}
						minValue={CONFIG.POSITION.MIN}
						maxValue={CONFIG.POSITION.MAX}
					/>
				</div>
			{/if}
		</div>

		<!-- Advanced Movement Settings -->
		<div class="border-t pt-2" style="border-color: var(--secondary-text);">
			<button
				class="flex items-center justify-between w-full text-sm --main-text hover:--main-text transition-colors"
				on:click={() => (showMovementSettings = !showMovementSettings)}
				aria-expanded={showMovementSettings}
			>
				<span>Movement Settings</span>
				<ChevronDown
					class="w-4 h-4 transition-transform duration-200"
					style={showMovementSettings ? 'transform: rotate(180deg)' : ''}
				/>
			</button>

			{#if showMovementSettings}
				<div class="mt-4 grid gap-6">
					<!-- Drag Option -->
					<Checkbox label="Drag" bind:checked={data.dragWhileMoving} {highlightColor} />

					<!-- Move Speed Configuration -->
					<div class="grid gap-4">
						<h4 class="text-sm font-medium --main-text">Move Speed</h4>
						<ButtonGroup variant="default">
							{#each SPEED_TYPES as type}
								<ButtonGroupItem
									value={type}
									on:click={() => (data.speed.type = type)}
									active={data.speed.type === type}
									itemHighlightColor={highlightColor}
								>
									{type}
								</ButtonGroupItem>
							{/each}
						</ButtonGroup>

						{#if data.speed.type === 'Human'}
							<TimeInput
								label="Speed"
								bind:value={data.speed.value}
								defaultValue={CONFIG.SPEED.DEFAULT}
								startingUnit="ms"
								{highlightColor}
							/>
							<div class="flex items-center gap-4">
								<Checkbox label="Randomize" bind:checked={data.speed.randomize} {highlightColor} />
								{#if data.speed.randomize}
									<div class="flex-1">
										<Slider
											label="Variance"
											unit="%"
											bind:value={data.speed.variance}
											min={CONFIG.VARIANCE.MIN}
											max={CONFIG.VARIANCE.MAX}
											defaultValue={CONFIG.VARIANCE.DEFAULT}
										/>
									</div>
								{/if}
							</div>
						{/if}
					</div>

					<!-- Path Type Configuration -->
					<div class="grid gap-4">
						<h4 class="text-sm font-medium --main-text">Path Type</h4>
						<ButtonGroup variant="default">
							{#each PATH_TYPES as type}
								<ButtonGroupItem
									value={type}
									on:click={() => (data.pathType = type)}
									active={data.pathType === type}
									itemHighlightColor={highlightColor}
								>
									{type}
								</ButtonGroupItem>
							{/each}
						</ButtonGroup>
					</div>
				</div>
			{/if}
		</div>
	</div>
</NodeWrapper>
