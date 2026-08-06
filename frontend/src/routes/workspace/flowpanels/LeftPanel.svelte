<script lang="ts">
  import { Plus, Play, Palette, Keyboard } from "lucide-svelte";
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
      group: "Flow Control",
      nodes: [ 
        {
          type: 'StartNode',
          label: 'Start Node',
          icon: Play,
          id: 'start-node',
          component: StartNode,
          isExpanded: false,
          data: undefined,
        },
        {
          type: 'DelayNode',
          label: 'Delay Node',
          icon: Play,
          id: 'delay-node',
          component: DelayNode,
          isExpanded: false,
          data: undefined,
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
          data: undefined,
        }
      ]
    },
    {
      group: "Mouse Control",
      nodes: [
        {
          type: 'MouseClickNode',
          label: 'Click Node',
          icon: Play,
          id: 'click-node',
          component: MouseClickNode,
          isExpanded: false,
          data: undefined,
        },
        {
          type: 'MouseMoveNode',
          label: 'Move Node',
          icon: Play,
          id: 'move-node',
          component: MouseMoveNode,
          isExpanded: false,
          data: undefined,
        }
      ]
    },
    {
      group: "Keyboard Control",
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
          data: undefined,
        }
      ]
    }
  ];

  // Function to handle drag start event
  function onDragStart(event: DragEvent, nodeType: string) {
    event.dataTransfer?.setData("application/svelteflow", nodeType);
    event.dataTransfer?.setData("text/plain", nodeType);
    event.dataTransfer?.setDragImage(event.target as Element, 0, 0);
    event.dataTransfer!.effectAllowed = "move";
  }
</script>

<div class="left-panel" class:panel-open={isLeftPanelExpanded}>
  <div class="panel-spacing">
    <h2 class="text-lg font-semibold mb-4 flex-center flex-gap">
      <Plus class="flow-icon" />
      <span>Nodes</span>
    </h2>
    {#each availableNodes as group}
      <div class="node-group">
        <h3 class="text-sm font-medium text-secondary mb-2">{group.group}</h3>
        <ul>
          {#each group.nodes as node}
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