<!-- StartNode.svelte -->
<script lang="ts">
	import { Play } from 'lucide-svelte';
	import { Position } from '@xyflow/svelte';
	import { onMount, onDestroy } from 'svelte';
	import { slide, fade } from 'svelte/transition';
	import { cubicOut } from 'svelte/easing';
	import NodeWrapper from './nodeComponents/NodeWrapper.svelte';
	import ButtonGroup from './nodeComponents/ButtonGroup.svelte';
	import ButtonGroupItem from './nodeComponents/ButtonGroupItem.svelte';
	import type { ComponentType } from 'svelte';
	import type { HandleConfig, StartNodeData } from '$lib/stores/flow.svelte';
	import '$lib/index.scss';

	// Type definitions for OS-specific key mappings
	type OperatingSystem = 'windows' | 'macos' | 'linux';
	type SpecialKey = { key: string; label: string };

	// Configuration for special keys based on operating system
	const specialKeysByOS: Record<OperatingSystem, SpecialKey[]> = {
		windows: [
			{ key: 'Control', label: 'Ctrl' },
			{ key: 'Alt', label: 'Alt' },
			{ key: 'Shift', label: 'Shift' },
			{ key: 'Meta', label: 'Win' }
		],
		macos: [
			{ key: 'Meta', label: 'Cmd' },
			{ key: 'Option', label: 'Option' },
			{ key: 'Control', label: 'Ctrl' },
			{ key: 'Shift', label: 'Shift' }
		],
		linux: [
			{ key: 'Control', label: 'Ctrl' },
			{ key: 'Alt', label: 'Alt' },
			{ key: 'Shift', label: 'Shift' },
			{ key: 'Meta', label: 'Super' }
		]
	};

	// Node connection point configuration
	const handles: HandleConfig[] = [
		{ id: 'right', type: 'source', position: Position.Right, offsetY: 50 }
	];

	// Props with default values
	export let id: string;
	export let title: string = 'Start';
	export let icon: ComponentType = Play;
	export let color: string = 'bg-gradient-to-r from-blue-500 to-blue-600';
	export let highlightColor: string = 'bg-blue-500';
	export let isConnectable: boolean = true;

	const DEFAULT_DATA: StartNodeData = {
		macroKeys: []
	};

	// Svelte Flow passes the node's data payload here, not the whole node - and it
	// is the *same object* the graph holds, so every edit below lands in the graph
	// by reference and is what gets saved. The recorded macro therefore has to
	// live on `data`, never in a local `let`.
	//
	// That reference is the whole of the contract now. The graph's nodes are deep
	// `$state` (see `$lib/stores/flow.svelte`), so assigning `data.macroKeys` is an
	// ordinary tracked write and the edit *is* its own notification - nothing has
	// to be announced afterwards.
	export let data: StartNodeData = { ...DEFAULT_DATA };

	// Persisted nodes can predate newer fields, so backfill whatever is missing
	// without clobbering saved values. This has to mutate `data` in place:
	// reassigning it (`data = { ...DEFAULT_DATA, ...data }`) would detach this
	// component from the store's object and silently discard every later edit.
	// It also runs exactly once, so a saved macro is never overwritten by the
	// empty default the way a reactive statement would overwrite it.
	Object.assign(data, { ...DEFAULT_DATA, ...data });

	// Component state
	let isRecording: boolean = false;
	let osDetectionFailed: boolean = false;
	let currentOS: OperatingSystem;
	// A SvelteSet would be the runes answer, and this component is still
	// legacy. The template does read this, but `toggleSpecialKey` reassigns it
	// (`= new Set(selectedSpecialKeys)`) before mutating, which is what makes
	// the read update under Svelte 4 semantics. Worth revisiting as a SvelteSet
	// when this component moves to runes, and not before - the reassignment and
	// the reactive class are two answers to the same question.
	// eslint-disable-next-line svelte/prefer-svelte-reactivity
	let selectedSpecialKeys = new Set<string>();

	/**
	 * Detects the user's operating system based on user agent
	 * @returns {OperatingSystem} Detected operating system or 'windows' as fallback
	 */
	function detectOS(): OperatingSystem {
		const userAgent = navigator.userAgent.toLowerCase();
		if (userAgent.includes('win')) return 'windows';
		if (userAgent.includes('mac')) return 'macos';
		if (userAgent.includes('linux')) return 'linux';
		osDetectionFailed = true;
		return 'windows'; // Fallback
	}

	currentOS = detectOS();

	/**
	 * Toggles a special key's selected state
	 * @param {string} key - The key to toggle
	 */
	function toggleSpecialKey(key: string) {
		selectedSpecialKeys = new Set(selectedSpecialKeys);
		if (selectedSpecialKeys.has(key)) {
			selectedSpecialKeys.delete(key);
		} else {
			selectedSpecialKeys.add(key);
		}
	}

	// Macro recording functions
	function startRecording() {
		data.macroKeys = Array.from(selectedSpecialKeys);
		isRecording = true;
	}

	function stopRecording() {
		isRecording = false;
	}

	/**
	 * Handles keydown events during macro recording
	 * @param {KeyboardEvent} event - The keyboard event
	 */
	function handleKeyDown(event: KeyboardEvent) {
		if (!isRecording) return;
		event.preventDefault();

		const key = event.key;
		if (!data.macroKeys.includes(key)) {
			// A fresh array rather than a push, so the write is visible to
			// this file's legacy `$:` statements as well as to the graph's
			// deep `$state`.
			data.macroKeys = [...data.macroKeys, key];
		}
	}

	/**
	 * Handles keyup events during macro recording
	 * @param {KeyboardEvent} event - The keyboard event
	 */
	function handleKeyUp(event: KeyboardEvent) {
		if (!isRecording) return;

		if (!event.ctrlKey && !event.altKey && !event.shiftKey && !event.metaKey) {
			stopRecording();
		}
	}

	// Lifecycle hooks
	onMount(() => {
		window.addEventListener('keydown', handleKeyDown);
		window.addEventListener('keyup', handleKeyUp);
	});

	onDestroy(() => {
		window.removeEventListener('keydown', handleKeyDown);
		window.removeEventListener('keyup', handleKeyUp);
	});

	// Reactive declarations
	$: macroDisplay = data.macroKeys.join('+');
</script>

<NodeWrapper {icon} {title} {color} {handles} {isConnectable} {id} type="StartNode">
	<div class="space-y-4" transition:slide|local={{ duration: 300, easing: cubicOut }}>
		<!-- OS Selection -->
		{#if osDetectionFailed}
			<div class="flex flex-col" transition:slide|local={{ duration: 300 }}>
				<label for="os-select" class="text-sm font-medium text-[--secondary-text]">
					Select Operating System:
				</label>
				<select
					id="os-select"
					bind:value={currentOS}
					class="mt-1 block w-full px-3 py-2 bg-[--secondary-hover] rounded-md shadow-sm focus:outline-none focus:ring-blue-500 transition-all duration-200"
				>
					<option value="windows">Windows</option>
					<option value="macos">macOS</option>
					<option value="linux">Linux</option>
				</select>
			</div>
		{/if}

		<!-- Special Keys Selection -->
		<div class="flex flex-col">
			<ButtonGroup variant="default">
				{#each specialKeysByOS[currentOS] as specialKey (specialKey.key)}
					<ButtonGroupItem
						value={specialKey.key}
						on:click={() => toggleSpecialKey(specialKey.key)}
						active={selectedSpecialKeys.has(specialKey.key)}
						itemHighlightColor={highlightColor}
					>
						{specialKey.label}
					</ButtonGroupItem>
				{/each}
			</ButtonGroup>
		</div>

		<!-- Macro Recording Controls -->
		<div class="flex flex-col">
			<div id="macro-controls" class="flex items-center space-x-2">
				<button
					on:click={isRecording ? stopRecording : startRecording}
					class={`px-3 py-2 rounded-md shadow-sm focus:outline-none transition-all duration-300 ${
						isRecording
							? 'bg-red-500 text-[--main-text] animate-pulse'
							: 'bg-[--main] text-[--secondary-text] hover:bg-[--main-hover]'
					}`}
				>
					{isRecording ? 'Recording...' : 'Record'}
				</button>
				<span
					class="px-3 py-2 bg-[--main-hover] bg-opacity-50 rounded-md shadow-sm transition-all duration-200"
					in:fade={{ duration: 200 }}
				>
					{data.macroKeys.length ? macroDisplay : 'No macro'}
				</span>
			</div>
		</div>
	</div>
</NodeWrapper>

<style>
	@keyframes pulse {
		0%,
		100% {
			opacity: 1;
		}
		50% {
			opacity: 0.7;
		}
	}

	:global(.animate-pulse) {
		animation: pulse 2s cubic-bezier(0.4, 0, 0.6, 1) infinite;
	}
</style>
