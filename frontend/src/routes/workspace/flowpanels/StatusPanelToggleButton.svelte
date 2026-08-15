<script lang="ts">
	import { ChevronLeft, ChevronRight } from 'lucide-svelte';
	export let isStatusPanelExpanded;
	export let hasStatusPanel;
	export let toggleStatusPanel;
</script>

{#if hasStatusPanel}
	<div class="status-toggle-button transition-all duration-300" class:opacity-0={!hasStatusPanel}>
		<button
			class="status-toggle-button-inner"
			style="right: {isStatusPanelExpanded ? '300px' : '0'}"
			on:click={toggleStatusPanel}
			aria-label={isStatusPanelExpanded ? 'Collapse status panel' : 'Expand status panel'}
		>
			<svelte:component
				this={isStatusPanelExpanded ? ChevronRight : ChevronLeft}
				class="flow-icon"
			/>
		</button>
	</div>
{/if}
