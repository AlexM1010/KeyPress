<!-- Select.svelte -->
<script lang="ts">
	import type { SvelteComponent } from 'svelte';

	export let label: string;
	export let options: string[];
	export let icon: typeof SvelteComponent<any> | null = null;
	export let value: string = '';

	// The DOM id of the <select>, tying it to its <label for=...>. It used to be
	// the hard-coded literal "select-input", so every instance on the page
	// rendered the same id: with two Selects in one node the document had
	// duplicate ids and *both* labels resolved to the first control, so clicking
	// the second label focused the wrong one. Defaulting to a fresh unique value
	// per instance (the same approach Slider.svelte already takes) keeps the
	// label/control pairing correct however many are on screen, while callers
	// that need a stable, predictable id can still pass one in.
	export let id: string = `select-${crypto.randomUUID()}`;
</script>

<div class="space-y-1.5">
	<label for={id} class="block text-xs font-medium --main-text">{label}</label>
	<div class="relative">
		{#if icon}
			<div class="absolute left-3 top-1/2 transform -translate-y-1/2 --main-text">
				<svelte:component this={icon} class="w-4 h-4" />
			</div>
		{/if}
		<!-- The left padding only clears an icon when there is one to clear. -->
		<select
			{id}
			bind:value
			class="select-input w-full pr-3 py-2 text-sm rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 {icon
				? 'pl-10'
				: 'pl-3'}"
		>
			{#each options as opt (opt)}
				<option value={opt}>{opt}</option>
			{/each}
		</select>
	</div>
</div>

<style>
	/* Themed like the number and time inputs the other nodes use, rather than
       the fixed light grey this carried before: a node sits on a translucent
       card that is dark under the dark theme, so a hard-coded light control was
       the one piece of a node that ignored the theme. */
	.select-input {
		background-color: var(--main);
		color: var(--main-text);
		border: 1px solid var(--border);
		transition: background-color 0.3s;
	}

	.select-input:hover {
		background-color: var(--main-hover);
	}

	/* The dropdown list is painted by the browser, which does not inherit the
       control's colours into it on every platform. */
	.select-input option {
		background-color: var(--secondary);
		color: var(--main-text);
	}
</style>
