<script lang="ts">
	import { setContext } from 'svelte';
	import { Play, Palette, Keyboard } from 'lucide-svelte';
	import MouseClickNode from '$lib/components/Workspace/customNodes/MouseClickNode.svelte';
	import MouseMoveNode from '$lib/components/Workspace/customNodes/MouseMoveNode.svelte';
	import StartNode from '$lib/components/Workspace/customNodes/StartNode.svelte';
	import DelayNode from '$lib/components/Workspace/customNodes/DelayNode.svelte';
	import ColorPickerNode from '$lib/components/Workspace/customNodes/ColorPickerNode.svelte';
	import KeyPressNode from '$lib/components/Workspace/customNodes/KeyPressNode.svelte';

	// The panel stays mounted whether or not it is showing - it slides in and out
	// on this, rather than being added to and removed from the page.
	export let isLeftPanelExpanded = true;

	export let availableNodes = [
		{
			group: 'Flow Control',
			nodes: [
				{
					type: 'StartNode',
					label: 'Start Node',
					icon: Play,
					id: 'start-node',
					component: StartNode,
					isExpanded: false,
					data: undefined
				},
				{
					type: 'DelayNode',
					label: 'Delay Node',
					icon: Play,
					id: 'delay-node',
					component: DelayNode,
					isExpanded: false,
					data: undefined
				},
				{
					// Must be 'ColorPickerNode': that is the key in customNodes/nodeTypes.ts
					// and the case the Go dispatcher in tasks.go matches on. Any other
					// spelling drops a node the backend rejects as an unknown task type.
					type: 'ColorPickerNode',
					label: 'Wait For Color',
					icon: Palette,
					id: 'color-picker-node',
					component: ColorPickerNode,
					isExpanded: false,
					// Left undefined like the others: the node backfills its own
					// DEFAULT_DATA, so the palette does not duplicate the payload.
					data: undefined
				}
			]
		},
		{
			group: 'Mouse Control',
			nodes: [
				{
					type: 'MouseClickNode',
					label: 'Click Node',
					icon: Play,
					id: 'click-node',
					component: MouseClickNode,
					isExpanded: false,
					data: undefined
				},
				{
					type: 'MouseMoveNode',
					label: 'Move Node',
					icon: Play,
					id: 'move-node',
					component: MouseMoveNode,
					isExpanded: false,
					data: undefined
				}
			]
		},
		{
			group: 'Keyboard Control',
			nodes: [
				{
					// Must be 'KeyPressNode': that is the key in customNodes/nodeTypes.ts
					// and the case the Go dispatcher in tasks.go matches on. Any other
					// spelling drops a node the backend rejects as an unknown task type.
					type: 'KeyPressNode',
					label: 'Keypress Node',
					icon: Keyboard,
					id: 'keypress-node',
					component: KeyPressNode,
					isExpanded: false,
					// Left undefined like the others: the node backfills its own
					// DEFAULT_DATA, so the palette does not duplicate the payload.
					data: undefined
				}
			]
		}
	];

	// Every node below is a drag preview, not a node in the flow. NodeWrapper reads
	// this to leave off the duplicate/delete menu, which would act on nothing here.
	setContext('nodePreview', true);

	// Builds the picture the cursor carries during a drag.
	//
	// Handing the <li> straight to setDragImage produced a translucent box round
	// the node: the <li> is the full width of the panel and taller than the
	// scaled-down preview inside it, and the node's own glass background
	// (rgba white + backdrop-filter) has nothing behind it in a drag snapshot, so
	// it washes out. Snapshotting a clone of just the node, on a slab of the
	// panel colour, makes the drag image the node exactly as it sits in the list.
	function buildDragImage(node: HTMLElement): HTMLElement {
		const rect = node.getBoundingClientRect();
		// rect is post-transform, offsetWidth is not, so this recovers whatever
		// scale .node-preview applies without hard-coding it here.
		const scale = node.offsetWidth ? rect.width / node.offsetWidth : 1;

		const clone = node.cloneNode(true) as HTMLElement;
		clone.style.width = `${node.offsetWidth}px`;
		clone.style.margin = '0';
		clone.style.transform = `scale(${scale})`;
		clone.style.transformOrigin = 'top left';

		const ghost = document.createElement('div');
		ghost.className = 'node-drag-ghost';
		ghost.style.width = `${rect.width}px`;
		ghost.style.height = `${rect.height}px`;
		ghost.appendChild(clone);
		document.body.appendChild(ghost);

		return ghost;
	}

	// Function to handle drag start event
	function onDragStart(event: DragEvent, nodeType: string) {
		event.dataTransfer?.setData('application/svelteflow', nodeType);
		event.dataTransfer?.setData('text/plain', nodeType);

		const node = (event.currentTarget as HTMLElement).querySelector<HTMLElement>(
			'.node-preview > *'
		);
		if (node && event.dataTransfer) {
			const rect = node.getBoundingClientRect();
			const ghost = buildDragImage(node);
			// Offsets keep the node under the same point of it the pointer grabbed.
			event.dataTransfer.setDragImage(ghost, event.clientX - rect.left, event.clientY - rect.top);
			// The browser snapshots the element during setDragImage, so the clone
			// only needs to outlive this frame.
			requestAnimationFrame(() => ghost.remove());
		}

		event.dataTransfer!.effectAllowed = 'move';
	}
</script>

<div class="left-panel" class:panel-open={isLeftPanelExpanded}>
	<div class="panel-spacing">
		{#each availableNodes as group (group.group)}
			<div class="node-group">
				<h3 class="text-sm font-medium text-secondary mb-2">{group.group}</h3>
				<ul class="node-list">
					{#each group.nodes as node (node.id)}
						<li
							class="draggable-node"
							draggable="true"
							on:dragstart={(event) => onDragStart(event, node.type)}
						>
							<div class="node-preview">
								<svelte:component this={node.component} id={node.id} data={node.data} />
							</div>
						</li>
					{/each}
				</ul>
				<div class="group-separator"></div>
			</div>
		{/each}
	</div>
</div>
