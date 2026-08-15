<!-- MouseClickNode.svelte -->
<script lang="ts">
    import { Position } from "@xyflow/svelte";
    import type { ComponentType } from 'svelte';
    import { MousePointer, ChevronDown } from 'lucide-svelte';
    import NodeWrapper from './nodeComponents/NodeWrapper.svelte';
    import Checkbox from "./nodeComponents/Checkbox.svelte";
    import ButtonGroup from "./nodeComponents/ButtonGroup.svelte";
    import ButtonGroupItem from "./nodeComponents/ButtonGroupItem.svelte";
    import TimeInput from "./nodeComponents/TimeInput.svelte";
    import NumberInput from './nodeComponents/NumberInput.svelte';
    import type { HandleConfig, MouseClickNodeData } from '$lib/stores/flow.svelte';

    type ButtonType = 'left' | 'middle' | 'right';
    type ScrollDirection = 'Vertical' | 'Horizontal';

    export let id: string;
    export let title: string = 'Mouse Click';
    export let icon: ComponentType = MousePointer;
    export let color: string = 'bg-gradient-to-r from-green-500 to-green-600';
    export let highlightColor: string = 'bg-green-500';

    const BUTTON_TYPES: ButtonType[] = ['left', 'middle', 'right'];
    const SCROLL_DIRECTIONS: ScrollDirection[] = ['Vertical', 'Horizontal'];

    const DEFAULT_DATA: MouseClickNodeData = {
        buttonType: 'left',
        numberOfClicks: 1,
        clickDelay: 0.1,
        pressReleaseDelay: 100,
        releaseAfterPress: true,
        scrollDirection: ['Vertical'],
        scrollLines: 0
    };

    // Svelte Flow passes the node's data payload here, not the whole node - and it
    // is the *same object* the graph holds, so every edit below lands in the graph
    // by reference and is what gets saved.
    //
    // That reference is the whole of the contract now. The graph's nodes are deep
    // `$state` (see `$lib/stores/flow.svelte`), so writing a property of `data` in
    // place is an ordinary tracked write and the edit *is* its own notification.
    // `toggleDirection`'s `push` included, and that is the one worth naming: it
    // changes no reference, so under the old store it slipped through unannounced
    // and only the `?? []` assignment on the line above it saved the edit.
    export let data: MouseClickNodeData = { ...DEFAULT_DATA };

    // Persisted nodes can predate newer fields, so backfill whatever is missing
    // without clobbering saved values. This has to mutate `data` in place:
    // reassigning it (`data = { ...DEFAULT_DATA, ...data }`) would detach this
    // component from the store's object and silently discard every later edit.
    Object.assign(data, { ...DEFAULT_DATA, ...data });

    let showAdvanced = false;

    $: console.log('MouseClickNode data:', JSON.stringify(data));

    const NODE_HANDLES: HandleConfig[] = [
        { id: "right", type: "source", position: Position.Right, offsetY: 50 },
        { id: "left", type: "target", position: Position.Left, offsetY: 50 },
    ];

    function toggleDirection(direction: ScrollDirection): void {
        data.scrollDirection = data.scrollDirection ?? [];
        if (data.scrollDirection.includes(direction)) {
            data.scrollDirection = data.scrollDirection.filter(d => d !== direction);
        } else {
            data.scrollDirection.push(direction);
        }
    }

    function updateButtonType(newType: ButtonType): void {
        data.buttonType = newType;
    }

    function toggleAdvancedOptions(): void {
        showAdvanced = !showAdvanced;
    }

    function handleClick(type: ButtonType): void {
        updateButtonType(type);
    }

    // Svelte Flow's NodeWrapper passes a fixed prop set (selected, isConnectable,
    // positionAbsoluteX, ...) to every custom node. Referencing $$restProps silences
    // the "created with unknown prop" warnings for the ones we don't declare.
    $$restProps;
</script>

<NodeWrapper
    {id}
    {icon}
    {title}
    {color}
    type="Click"
    handles={NODE_HANDLES}
>
    <div class="grid gap-6">
        <!-- Button Type Selection -->
        <ButtonGroup variant="default">
            {#each BUTTON_TYPES as type}
                <ButtonGroupItem 
                    value={type}
                    on:click={() => handleClick(type)}
                    active={data.buttonType === type}
                    itemHighlightColor={highlightColor}
                >
                    {type.charAt(0).toUpperCase() + type.slice(1)}
                </ButtonGroupItem>
            {/each}
        </ButtonGroup>

        <!-- Click Configuration.

             These two rows are the widest thing any node holds, and nodes are now
             a fixed width (`nodeComponents/NodeWrapper.svelte`) rather than one
             that grows to fit. They fit as they are, but `flex-wrap` means a
             longer number - or a unit swapped to `min` - drops onto a second line
             instead of spilling out past the card. -->
        <div class="grid gap-6 auto-rows-min">
            <div class="flex flex-wrap justify-between items-center gap-2">
                <NumberInput
                    label="Clicks"
                    bind:value={data.numberOfClicks}
                    minValue={0} 
                    maxValue={1000}
                />
                {#if data?.numberOfClicks > 1}
                    <TimeInput 
                        label="delay" 
                        bind:value={data.clickDelay}
                        defaultValue={0.1}
                        highlightColor={highlightColor}
                    />
                {/if}
            </div>
        
            <div class="flex flex-wrap justify-between items-center gap-2">
                <Checkbox
                    label="Release"
                    bind:checked={data.releaseAfterPress}
                    highlightColor={highlightColor}
                />
                {#if data?.releaseAfterPress}
                    <TimeInput 
                        label="after" 
                        bind:value={data.pressReleaseDelay}
                        defaultValue={0.1}
                        highlightColor={highlightColor}
                    />
                {/if}
            </div>
        </div>
        
        <!-- Advanced Options Section -->
        <div class="border-t pt-2" style="border-color: var(--secondary-text);">
            <button
                class="flex items-center justify-between w-full text-sm text-[--secondary-text] hover:text-[--secondary-text-hover] transition-colors"
                on:click={toggleAdvancedOptions}
                aria-expanded={showAdvanced}
            >
                <span>Scroll Options</span>
                <ChevronDown
                    class="w-4 h-4 transition-transform duration-200"
                    style={showAdvanced ? "transform: rotate(180deg)" : ""}
                />
            </button>

            {#if showAdvanced}
                <div class="mt-4 grid gap-6">
                    <ButtonGroup variant="default">
                        {#each SCROLL_DIRECTIONS as direction}
                            <ButtonGroupItem 
                                value={direction}
                                on:click={() => toggleDirection(direction)}
                                active={data.scrollDirection.includes(direction)}
                                itemHighlightColor={highlightColor}
                            >
                                {direction}
                            </ButtonGroupItem>
                        {/each}
                    </ButtonGroup>
                    
                    <NumberInput
                        label="Lines"
                        bind:value={data.scrollLines}
                        minValue={-100000}
                        maxValue={100000}
                    />
                </div>
            {/if}
        </div>
    </div>
</NodeWrapper>