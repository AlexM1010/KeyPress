<script lang="ts">
	import { SvelteFlowProvider } from '@xyflow/svelte';
	import Flow from './workspace/Flow.svelte';
</script>

<div class="flow-wrapper h-screen w-full overflow-hidden">
	<SvelteFlowProvider>
		<Flow />
	</SvelteFlowProvider>
</div>

<style>
	.flow-wrapper {
		position: fixed;
		left: 0;
		right: 0;
	}
</style>
