<script lang="ts">
	import { Check, X, Play, TriangleAlert } from 'lucide-svelte';
	export let isStatusPanelExpanded;
	/**
	 * `message` is already user-facing text - nodes are named the way the canvas
	 * names them ("Delay 2 completed successfully."), never by their raw id.
	 * `nodeId` carries that id separately for the messages that have one, so it
	 * stays reachable for debugging without cluttering the panel.
	 */
	export let statusMessages: {
		id: string;
		type: string;
		message: string;
		nodeId?: string;
	}[];
	export let executionStatus;
</script>

<div class="status-panel" class:panel-open={isStatusPanelExpanded}>
	<div class="panel-spacing">
		<h2 class="text-lg font-semibold mb-4 flex-center flex-gap">
			<svelte:component this={executionStatus.icon} class="flow-icon {executionStatus.color}" />
			<span>Execution Status</span>
		</h2>
		{#if statusMessages.length === 0}
			<p class="text-[--secondary-text]">No status updates.</p>
		{:else}
			<ul>
				{#each statusMessages as msg (msg.id)}
					<li class="flex-center mb-3">
						{#if msg.type === 'success'}
							<Check class="flow-icon text-green-500 mr-2" />
						{:else if msg.type === 'error'}
							<X class="flow-icon text-red-500 mr-2" />
						{:else if msg.type === 'warning'}
							<TriangleAlert class="flow-icon text-yellow-500 mr-2" />
						{:else if msg.type === 'info' || msg.type === 'running'}
							<Play
								class="flow-icon text-blue-500 mr-2 {msg.type === 'running' ? 'animate-pulse' : ''}"
							/>
						{/if}
						<span class="text-sm" title={msg.nodeId ? `Node id: ${msg.nodeId}` : undefined}
							>{msg.message}</span
						>
					</li>
				{/each}
			</ul>
		{/if}
	</div>
</div>
