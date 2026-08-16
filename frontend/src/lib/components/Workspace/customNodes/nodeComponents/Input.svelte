<script lang="ts">
	import type { ComponentType } from 'svelte';

	type Props = {
		label: string;
		type?: string;
		defaultValue?: string;
		icon?: ComponentType | null;
	};

	// No `$bindable` on anything here, unlike the other boxes in this directory:
	// the input is rendered with `value={defaultValue}` and never reads back, so
	// nothing has ever bound to this component. Nothing renders it either - it is
	// unreferenced across the app, and is converted only so this directory is not
	// left one file short of runes.
	let {
		label,
		type = 'text',
		defaultValue = '',
		// Capitalised so it renders as `<Icon />`, retiring the `<svelte:component>`
		// this needed before - the same shape Select.svelte now uses.
		icon: Icon = null
	}: Props = $props();
</script>

<div class="space-y-1.5">
	<label for="inputField" class="block text-xs font-medium --main-text">{label}</label>
	<div class="relative">
		{#if Icon}
			<div class="absolute left-3 top-1/2 transform -translate-y-1/2 --main-text">
				<Icon class="w-4 h-4" />
			</div>
		{/if}
		<input
			id="inputField"
			{type}
			value={defaultValue}
			class="w-full pr-3 py-2 pl-10 text-sm bg-gray-50 border border-gray-200 rounded-lg focus:ring-2 focus:ring-blue-400 focus:border-transparent"
		/>
	</div>
</div>
