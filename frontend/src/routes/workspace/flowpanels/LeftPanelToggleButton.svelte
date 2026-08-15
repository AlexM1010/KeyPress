<script lang="ts">
	import { ChevronLeft, ChevronRight } from 'lucide-svelte';
	export let isLeftPanelExpanded;
	export let toggleLeftPanel;
</script>

<div class="left-toggle-button">
	<button
		class="left-toggle-button-inner"
		style="left: {isLeftPanelExpanded ? '300px' : '0'}"
		on:click={toggleLeftPanel}
		aria-label={isLeftPanelExpanded ? 'Collapse node panel' : 'Expand node panel'}
	>
		<svelte:component this={isLeftPanelExpanded ? ChevronLeft : ChevronRight} class="flow-icon" />
	</button>
</div>
