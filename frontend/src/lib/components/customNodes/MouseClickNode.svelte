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
    import type { HandleConfig, MouseClickNodeData } from '$lib/stores/flow';

    type ButtonType = 'left' | 'middle' | 'right';
    type ScrollDirection = 'Vertical' | 'Horizontal';

    export let id: string;
    export let title: string = 'Mouse Click';
    export let icon: ComponentType = MousePointer;
    export let color: string = 'bg-gradient-to-r from-green-500 to-green-600';
    export let highlightColor: string = 'bg-green-500';

    const BUTTON_TYPES: ButtonType[] = ['left', 'middle', 'right'];
    const SCROLL_DIRECTIONS: ScrollDirection[] = ['Vertical', 'Horizontal'];

    export let data: MouseClickNodeData = {
        id: '',
        type: 'MouseClickNode',
        position: { x: 0, y: 0 },
        data: {
            buttonType: 'left',
            numberOfClicks: 1,
            clickDelay: 0.1,
            pressReleaseDelay: 100,
            releaseAfterPress: true,
            scrollDirection: ['Vertical'],
            scrollLines: 0
        }
    };

    // Update all data references to use data.data
    $: {
        if (!data.data) {
            data.data = {
                buttonType: 'left',
                numberOfClicks: 1,
                clickDelay: 0.1,
                pressReleaseDelay: 100,
                releaseAfterPress: true,
                scrollDirection: ['Vertical'],
                scrollLines: 0
            };
        }
    }

    let showAdvanced = false;

    $: console.log('MouseClickNode data:', JSON.stringify(data));

    const NODE_HANDLES: HandleConfig[] = [
        { id: "right", type: "source", position: Position.Right, offsetY: 50 },
        { id: "left", type: "target", position: Position.Left, offsetY: 50 },
    ];

    function handleDuplicate(): void {
        console.log("Duplicate action triggered", JSON.stringify(data));
    }

    function handleDelete(): void {
        console.log("Delete action triggered", JSON.stringify(data));
    }

    function toggleDirection(direction: ScrollDirection): void {
        data.data.scrollDirection = data.data.scrollDirection ?? [];
        if (data.data.scrollDirection.includes(direction)) {
            data.data.scrollDirection = data.data.scrollDirection.filter(d => d !== direction);
        } else {
            data.data.scrollDirection.push(direction);
        }
    }

    function updateButtonType(newType: ButtonType): void {
        data.data.buttonType = newType;
    }

    function toggleAdvancedOptions(): void {
        showAdvanced = !showAdvanced;
    }

    function handleClick(type: ButtonType): void {
        updateButtonType(type);
    }
</script>

<NodeWrapper
    {id}
    {icon}
    {title}
    {color}
    type="Click"
    handles={NODE_HANDLES}
    bind:data
    on:duplicate={handleDuplicate}
    on:delete={handleDelete}
>
    <div class="grid gap-6">
        <!-- Button Type Selection -->
        <ButtonGroup variant="default">
            {#each BUTTON_TYPES as type}
                <ButtonGroupItem 
                    value={type}
                    on:click={() => handleClick(type)}
                    active={data.data.buttonType === type}
                    itemHighlightColor={highlightColor}
                >
                    {type.charAt(0).toUpperCase() + type.slice(1)}
                </ButtonGroupItem>
            {/each}
        </ButtonGroup>

        <!-- Click Configuration -->
        <div class="grid gap-6 auto-rows-min">
            <div class="flex justify-between items-center gap-2">
                <NumberInput
                    label="Clicks"
                    bind:value={data.data.numberOfClicks}
                    minValue={0} 
                    maxValue={1000}
                />
                {#if data?.data.numberOfClicks > 1}
                    <TimeInput 
                        label="delay" 
                        bind:value={data.data.clickDelay}
                        defaultValue={0.1}
                        highlightColor={highlightColor}
                    />
                {/if}
            </div>
        
            <div class="flex justify-between items-center gap-2">
                <Checkbox
                    label="Release"
                    bind:checked={data.data.releaseAfterPress}
                    highlightColor={highlightColor}
                />
                {#if data?.data.releaseAfterPress}
                    <TimeInput 
                        label="after" 
                        bind:value={data.data.pressReleaseDelay}
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
                                active={data.data.scrollDirection.includes(direction)}
                                itemHighlightColor={highlightColor}
                            >
                                {direction}
                            </ButtonGroupItem>
                        {/each}
                    </ButtonGroup>
                    
                    <NumberInput
                        label="Lines"
                        bind:value={data.data.scrollLines}
                        minValue={-100000}
                        maxValue={100000}
                    />
                </div>
            {/if}
        </div>
    </div>
</NodeWrapper>